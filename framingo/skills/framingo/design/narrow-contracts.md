# Ask for the narrowest contract

**Decision.** Every dependency is typed as the smallest interface that
serves the one holding it.

- **A service's own `Repository`.** Each service declares in its `model.go`
  the `repo.*` contracts it reads and writes through (the user service:
  `repo.User` plus `repo.Organization`). One repository instance implements
  them all, and the wiring hands it over as is — the interface, not the
  instance, says what the service may touch.
- **Peers by `model.*`, collaborators by `common.*`.** The example service
  takes `common.RawMessageSender`, not `messagebus.Manager`; the auth service
  takes `model.UserAuthN`, not the user service.
- **No service package is imported for its type.** A router, a middleware
  and a service each hold a `types/model` interface — `model.Role` in
  `authz`, `model.Platform` first in every service — so only the wiring, and
  a test building the real thing, imports `pkg/services/...`. The lint
  config's depguard rules hold this ([checks.md](checks.md)).
- **Message handlers are `Named`, not `Service`.** `common.MessageHandler`
  and `RawMessageHandler` embed `common.Named`: the bus registers the
  handler itself and keys its subscription on the name, which is all it
  asks. A handler need not declare `Dependencies` to take messages.

**Why.** A narrow contract documents the dependency in the signature, keeps
fakes small in tests (embed the interface, implement what the test drives),
and lets the provider change everything the consumer does not use.

**Cost.** More small interfaces; a service that grows a need widens its
`Repository` in its own `model.go`, which is the point — the widening shows
in review.
