package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"
)

func TestLayerScanRefusesCredentialContentWithoutPrintingIt(t *testing.T) {
	secret := "-----BEGIN PRIVATE KEY-----\nsynthetic-test-only\n-----END PRIVATE KEY-----\n"
	_, err := scanLayer("synthetic/layer.tar", syntheticLayer("tmp/dummy.txt", secret))
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("scan error = %v", err)
	}
	if err != nil && contains(err.Error(), "synthetic-test-only") {
		t.Fatalf("finding printed matching content: %v", err)
	}
}

func TestLayerScanAcceptsOrdinaryRuntimeFiles(t *testing.T) {
	files, err := scanLayer("synthetic/layer.tar", syntheticLayer("usr/local/bin/tracker-web", "ordinary binary-shaped bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("files = %d", files)
	}
}

func TestLayerScanReadsGzipCompressedDockerLayers(t *testing.T) {
	secret := "-----BEGIN PRIVATE KEY-----\nsynthetic-test-only\n-----END PRIVATE KEY-----\n"
	_, err := scanLayer("synthetic/layer.tar.gz", syntheticGzipLayer("tmp/dummy.txt", secret))
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("scan error = %v", err)
	}
}

func contains(text, fragment string) bool {
	for index := 0; index+len(fragment) <= len(text); index++ {
		if text[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func syntheticLayer(path, contents string) io.Reader {
	var buffer bytes.Buffer
	writeSyntheticTar(&buffer, path, contents)
	return &buffer
}

func syntheticGzipLayer(path, contents string) io.Reader {
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writeSyntheticTar(compressed, path, contents)
	_ = compressed.Close()
	return &buffer
}

func writeSyntheticTar(destination io.Writer, path, contents string) {
	writer := tar.NewWriter(destination)
	_ = writer.WriteHeader(&tar.Header{Name: path, Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte(contents))
	_ = writer.Close()
}
