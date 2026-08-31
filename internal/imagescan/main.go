// Command imagescan mechanically checks every filesystem layer in a Docker image for
// credential-shaped content. Findings name only the rule and path, never the matching bytes.
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const maximumScannedFile = 64 << 20

var credentialPatterns = []struct {
	name       string
	expression *regexp.Regexp
}{
	{name: "private-key", expression: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)},
	{name: "gcp-service-account-private-key", expression: regexp.MustCompile(`"private_key"[[:space:]]*:`)},
	{name: "aws-access-key", expression: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{name: "github-token", expression: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`)},
	{name: "google-oauth-token", expression: regexp.MustCompile(`ya29\.[A-Za-z0-9_-]{20,}`)},
}

var credentialPaths = regexp.MustCompile(`(^|/)(\.env|application_default_credentials\.json|service-account\.json|id_rsa|id_ed25519)$`)

var ErrCredential = errors.New("credential-shaped content found")

type finding struct {
	Layer string
	Path  string
	Rule  string
}

func (found finding) Error() string {
	return fmt.Sprintf("%v: layer %s file %s matched %s", ErrCredential, found.Layer, found.Path, found.Rule)
}

func (found finding) Unwrap() error { return ErrCredential }

type scanCount struct {
	layers int
	files  int
}

func main() {
	selfTest := flag.Bool("self-test", false, "prove a dummy private key added to the image is refused")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: imagescan [--self-test] IMAGE")
		os.Exit(2)
	}
	image := flag.Arg(0)
	if *selfTest {
		if err := runSelfTest(image); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	count, err := scanImage(image)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("PASS: credential scan (%d layers, %d regular files)\n", count.layers, count.files)
}

func scanImage(image string) (scanCount, error) {
	temporary, err := os.MkdirTemp("", "work-tracker-image-scan-")
	if err != nil {
		return scanCount{}, err
	}
	defer os.RemoveAll(temporary)
	archive := filepath.Join(temporary, "image.tar")
	command := exec.Command("docker", "image", "save", "--output", archive, image)
	if output, err := command.CombinedOutput(); err != nil {
		return scanCount{}, fmt.Errorf("save image: %w: %s", err, strings.TrimSpace(string(output)))
	}
	layers, err := manifestLayers(archive)
	if err != nil {
		return scanCount{}, err
	}
	return scanArchive(archive, layers)
}

func manifestLayers(path string) (map[string]struct{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name != "manifest.json" {
			continue
		}
		var manifests []struct {
			Layers []string `json:"Layers"`
		}
		if err := json.NewDecoder(io.LimitReader(reader, header.Size)).Decode(&manifests); err != nil {
			return nil, fmt.Errorf("decode image manifest: %w", err)
		}
		if len(manifests) != 1 || len(manifests[0].Layers) == 0 {
			return nil, errors.New("image save did not contain exactly one layered manifest")
		}
		layers := make(map[string]struct{}, len(manifests[0].Layers))
		for _, layer := range manifests[0].Layers {
			layers[layer] = struct{}{}
		}
		return layers, nil
	}
	return nil, errors.New("image save has no manifest.json")
}

func scanArchive(path string, layers map[string]struct{}) (scanCount, error) {
	file, err := os.Open(path)
	if err != nil {
		return scanCount{}, err
	}
	defer file.Close()
	outer := tar.NewReader(file)
	count := scanCount{}
	for {
		header, err := outer.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return count, err
		}
		if _, included := layers[header.Name]; !included {
			continue
		}
		count.layers++
		files, err := scanLayer(header.Name, io.LimitReader(outer, header.Size))
		count.files += files
		if err != nil {
			return count, err
		}
	}
	if count.layers != len(layers) {
		return count, fmt.Errorf("scanned %d of %d image layers", count.layers, len(layers))
	}
	return count, nil
}

func scanLayer(layer string, contents io.Reader) (int, error) {
	buffered := bufio.NewReader(contents)
	magic, err := buffered.Peek(2)
	if err != nil {
		return 0, fmt.Errorf("read layer compression header: %w", err)
	}
	var layerContents io.Reader = buffered
	if magic[0] == 0x1f && magic[1] == 0x8b {
		compressed, err := gzip.NewReader(buffered)
		if err != nil {
			return 0, fmt.Errorf("open gzip layer: %w", err)
		}
		defer compressed.Close()
		layerContents = compressed
	}
	reader := tar.NewReader(layerContents)
	files := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return files, err
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		files++
		cleanPath := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(header.Name)), "./")
		if credentialPaths.MatchString(cleanPath) {
			return files, finding{Layer: layer, Path: cleanPath, Rule: "credential-filename"}
		}
		if header.Size > maximumScannedFile {
			return files, fmt.Errorf("refusing to skip oversized layer file %s (%d bytes)", cleanPath, header.Size)
		}
		body, err := io.ReadAll(io.LimitReader(reader, maximumScannedFile+1))
		if err != nil {
			return files, err
		}
		for _, pattern := range credentialPatterns {
			if pattern.expression.Match(body) {
				return files, finding{Layer: layer, Path: cleanPath, Rule: pattern.name}
			}
		}
	}
}

func runSelfTest(baseImage string) error {
	temporary, err := os.MkdirTemp("", "work-tracker-image-scan-self-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	dockerfile := "ARG BASE_IMAGE\nFROM ${BASE_IMAGE}\nUSER 0:0\nCOPY dummy-private-key.txt /tmp/dummy-private-key.txt\nUSER 65532:65532\n"
	dummy := "-----BEGIN PRIVATE KEY-----\nsynthetic-scanner-fixture-not-a-credential\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(temporary, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(temporary, "dummy-private-key.txt"), []byte(dummy), 0o600); err != nil {
		return err
	}
	mutatedImage := fmt.Sprintf("work-tracker:credential-scan-%d", os.Getpid())
	defer func() {
		remove := exec.Command("docker", "image", "rm", "--force", mutatedImage)
		remove.Stdout = io.Discard
		remove.Stderr = io.Discard
		_ = remove.Run()
	}()
	build := exec.Command("docker", "build", "--quiet", "--tag", mutatedImage,
		"--build-arg", "BASE_IMAGE="+baseImage, temporary)
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build credential mutation: %w: %s", err, strings.TrimSpace(string(output)))
	}
	_, scanErr := scanImage(mutatedImage)
	if !errors.Is(scanErr, ErrCredential) {
		return fmt.Errorf("credential scan accepted dummy private key: %v", scanErr)
	}
	var matched finding
	if !errors.As(scanErr, &matched) {
		return fmt.Errorf("credential scan returned unreportable finding: %v", scanErr)
	}
	fmt.Printf("PASS: credential scan self-test (dummy %s refused in %s and removed)\n", matched.Rule, matched.Path)
	return nil
}
