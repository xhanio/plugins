# HTTP Client — `pkg/services/api/client`

The framework transport: TLS, headers, cookies, body encoding, structured
errors. The app-facing SDK built on top of it lives in
[components-client.md](../app/components-client.md).

```go
import "github.com/xhanio/framingo/pkg/services/api/client"

cli := client.New("https://api.example.com",
    client.WithLogger(logger),
    client.WithTimeout(10*time.Second),
    // client.WithCert(certBundle, tls.RequireAndVerifyClientCert), // mTLS
    // client.WithDebug(),                                          // skip TLS verify - dev only
)
```

```go
type Client interface {
    common.Initializable
    SetHeaders(headers ...common.Pair[string, string])   // global; empty value deletes
    SetCookies(cookies ...*http.Cookie)
    NewRequest(ctx context.Context, request *Request, opts ...RequestOption) (*http.Request, error)
    Do(req *http.Request) (*http.Response, error)
    Send(ctx context.Context, request *Request, opts ...RequestOption) (*http.Response, error)  // NewRequest + Do
}
```

A `client.Request` carries `Method`, `Path`, `Headers` (`common.Pairs`),
`Cookies`, `ContentType`, `Body` (an `io.Reader`, `[]byte`, `string`, or —
with a JSON content type — any marshalable value), and `Encoding`
(`api.EncodingDeflate` compresses the body; the server's deflate middleware
inflates it). Per-request options: `WithRequestHeaders`, `WithRequestCookies`,
`WithRequestEncoding`.

Global headers and cookies set via `SetHeaders`/`SetCookies` ride every
request — that's how a session token attaches once after login.
