# Client Component — The App SDK

`pkg/components/client/<app>/` is the app-facing SDK: a typed,
credential-holding wrapper over the framework's
[HTTP client](../pkgs/client.md), one method per API operation. The
[operator CLI](components-cmd.md) and any other program that talks to the
app consume this component, never `client.Client` directly.

The real one is `example/pkg/components/client/example/`:

```
components/client/example/
├── model.go      # the Client interface
├── client.go     # cli struct, New(), Init() — build the transport, load the credential
├── option.go     # WithEndpoint / WithCredential / WithDebug
├── auth.go       # Credential persistence (0600) + Login/Logout
├── example.go    # HelloWorld — one file per API domain, one method per operation
└── messagebus.go # StreamMessages — WebSocket to /messages/stream
```

```go
// model.go
type Client interface {
	Init() error
	HelloWorld(ctx context.Context, message string) error
	Login(ctx context.Context, username, password string) error
	Logout(ctx context.Context) error
	StreamMessages(ctx context.Context) error
}
```

`Init` (no ctx — it is pre-request setup) builds the transport, loads any
saved credential, and installs the session header globally:

```go
// client.go
const DefaultEndpoint = "http://127.0.0.1:8080/api/v1"

func (c *cli) Init() error {
	if c.endpoint == "" {
		c.endpoint = DefaultEndpoint
	}
	c.cli = client.New(c.endpoint, c.opts...)
	if err := c.cli.Init(context.Background()); err != nil {
		return errors.Wrap(err)
	}
	if err := c.cred.Load(c.credFile); err != nil {
		return errors.Wrap(err)
	}
	c.cli.SetHeaders(common.NewPair(fapi.HeaderKeySession, c.cred.SessionID))
	return nil
}
```

Auth state lives here, not in callers. `Login` posts the credentials, reads
the session from the response header, persists it 0600 (it is a live
token), and installs it for every later call; `Logout` removes the file:

```go
// auth.go — the heart of Login
sessionID := resp.Header.Get(fapi.HeaderKeySession)
if sessionID == "" {
	return errors.Unauthorized.Newf("empty session id")
}
c.cred.SessionID = sessionID
if c.credFile != "" {
	if err := c.cred.Dump(c.credFile); err != nil { // MarshalIndent + WriteFile 0600
		return errors.Wrap(err)
	}
}
c.cli.SetHeaders(common.NewPair(fapi.HeaderKeySession, c.cred.SessionID))
```

Operations take `ctx` and typed DTOs from the project's `pkg/types/api`,
build a `client.Request`, `Send`, and decode — callers never see
`*http.Request`:

```go
// example.go
func (c *cli) HelloWorld(ctx context.Context, message string) error {
	body := &api.HelloWorldCreateRequest{Message: message}
	resp, err := c.cli.Send(ctx, &client.Request{
		Method:      http.MethodPost,
		Path:        "/example/helloworld",
		ContentType: echo.MIMEApplicationJSON,
		Body:        body,
	}, client.WithRequestEncoding(fapi.EncodingDeflate)) // the server's deflate middleware inflates it
	if err != nil {
		return errors.Wrap(err)
	}
	defer resp.Body.Close()
	// read and print the body
	return nil
}
```

`messagebus.go` is the WebSocket side: `StreamMessages` rewrites the HTTP
endpoint to `ws(s)://…/messages/stream`, dials with the session header, and
prints each received message until the context is canceled — sending a
proper close frame on shutdown so the server sees a clean disconnect.

See [layout.md](layout.md) for where components sit in the tree.
