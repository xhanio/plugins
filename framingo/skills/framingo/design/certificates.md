# Certificates: who holds the key, which CA signs

## A requester keeps its private key

**Decision.** An agent sends a CSR to `POST /certificates/csr`; the manager
signs it and answers with the certificate, never a key, and stores it
without one. The request — `common_name`, `subject_alt_name`, `usage` —
decides who the certificate is for: a CSR naming anything else is refused
(400), and its signature proves the sender holds the key. A signed
certificate lasts 90 days unless the request says otherwise.

**Why.** A key the manager generates and sends is one more copy to leak — in
the response, the database, a log. And the CA signs what the request grants,
not what the CSR asks, so a CSR cannot widen its own identity.

## One CA per purpose

**Decision.** Two independent self-signed CAs, `preset.ServerCAName` and
`preset.AgentCAName`. The server CA signs the manager's own certificates —
`Issue` without a `ca_id`; agents verify the manager against it. The agent
CA signs every CSR, and the manager verifies agents against it alone
(`authnagent`).

**Why.** With one CA, whoever may sign a CSR — `usage: server`, the
manager's hostname — holds a certificate every agent takes for the manager.
Split, a CSR's certificate chains only to the agent CA, which no agent
verifies the manager against. Each can also be rotated, or lost, without the
other.

## CA keys live in the database alone

**Decision.** `Init` loads both CAs, creating them on first start; they are
certificate records that `List` leaves out and `Delete` refuses. Nothing is
written to disk. What verifies against them takes their certificates:
`GET /certificates/ca` (`certificate_read`) returns both, and the CSR
response carries `server_ca` for the agent to trust the manager by.

**Why.** A key file beside the config is a copy outside the database's
access control, one `git add` from a commit under `env/`. Whoever verifies —
nginx's `ssl_client_certificate`, an agent's trust store — needs the
certificate, not the key. The cost: the keys are as safe as the database; a
production CA keeps its root offline and its keys in a KMS or HSM.

## A download is encoded as the caller asks

**Decision.** `POST /certificates/:id/download` takes a `format`: `pem`, the
certificate alone; `tar`, with its key, as `<name>.crt` and `<name>.key`; or
`p12`, with its key and chain. No format is tar with a key, pem without. A
p12 is encoded on download, by `certutil.EncodePKCS12`, with the password the
request carries: an uploaded PKCS#12 keeps neither its file nor its password.

**Why.** A stored password would sit beside the key it encrypts, which the
database holds anyway, so it would protect nothing; and the uploader's
password is not the downloader's to have. A stored original is a second copy
of the key to keep in step, often under algorithms OpenSSL 3 refuses without
`-legacy`. Encoded anew, a p12 is AES-256 with PBKDF2, and any certificate
with its key downloads as one, not only those uploaded as one.

## Each side verifies the other against the CAs it is given

**Decision.** `client.WithTLS(cert, rootCAs...)` verifies the server against
`rootCAs`, the system's when none — never not at all, and never against the
CAs of the client's own certificate. `server.WithTLS(cert, clientCAs...)`
asks for a client certificate and verifies one given against `clientCAs`
(`VerifyClientCertIfGiven`), without requiring it; with no CAs it asks for
none. A server whose certificate has no private key is refused by `Add`.

**Why.** An agent's certificate chains to the agent CA and the manager's to
the server CA: a client trusting its own issuer would never verify the
manager, and one skipping verification would trust anyone. One listener
serves users by session as well as agents by certificate, so requiring a
certificate would lock users out: the route decides who must present one, as
it decides who may call — and by then the handshake has checked the chain,
the expiry and the client-auth usage.

## Not yet

Trust is one certificate per CA, both CAs are self-signed, and an agent has
no way in of its own: no enrollment, renewal or revocation, and a
certificate is bound to no agent record. `authnagent` is not registered and
the example's server is given no agent CA. The repository's `ROADMAP.md`
plans each, in order.
