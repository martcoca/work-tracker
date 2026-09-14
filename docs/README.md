# Agentic Work Tracker — documentation

Everything needed to work on this product, in this repository. **Nothing here requires the
doctrine repository**, and the operating model that produced this product no longer governs
it.

## Read in this order

| | |
|---|---|
| [state-of-the-product.md](state-of-the-product.md) | **Start here.** What is live, what is broken, what is unbuilt — verified, not recalled. |
| [roadmap.md](roadmap.md) | What to do next, in order, and why — derived from the specification, not from `packets/`. |
| [product-specification.md](product-specification.md) | What the product is for, who its actors are, and what it must never do. |
| [technical-specification.md](technical-specification.md) | How it is built: components, interfaces, data, security, delivery. |
| [architecture.md](architecture.md) | The portfolio constraints it inherits, reproduced in full. |
| [decisions.md](decisions.md) | Every architecture decision that binds it, stated inline. |

## The product in three sentences

A packet is a unit of work with a frozen scope, an append-only history, and evidence. A human
signs in to read initiatives, epics and packets and to see what agents did; an agent
authenticates with a credential this product issued and writes back comments and status. The
product publishes what it knows as a verifiable file so a reader never has to call it.

## The constraints that actually shape it

- **No shared spine.** A session reads its packet from a published file, never by calling
  this product. The product can be entirely down and work continues.
- **Nobody edits a packet body.** Not a human, not an agent. A packet
  whose scope was wrong is superseded and the original stays as the record.
- **Deny by default**, server-side, on every route. Holding a credential is not authority;
  the named scope is.
- **Idle cost zero**, enforced by a plan-time guard rather than intended.
- **A check is not evidence until you have made it fail.** Removing a rule must break a test.

## Where the code is

```
surface/          HTTP routes, authentication, authorization
packet/           the packet model: events, projection, transitions
packetexport/     the published export: envelope, digest, freshness
agentcredential/  issuing and authenticating machine credentials
credentialstore/  Firestore persistence for credentials
eventstore/       Firestore persistence for packet events
identity/         human identity verification
web/src/          the Vue frontend
infra/            OpenTofu: deploy stack and trust
```

## Working on it

```bash
GOWORK=off go test ./... -count=1
GOWORK=off go vet ./...
npm test
npm run build
```

Deploy is keyless on merge to `main` and takes about three minutes. Rollback is a workflow
dispatch with a full commit, and it moves the frontend and the API together.
