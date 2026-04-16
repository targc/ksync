# HTTP Connection Reuse in Go

## How it works

Go's `net/http` transport maintains a pool of persistent TCP connections (keep-alive).
For a connection to be returned to the pool after a request, the transport must know
the response is fully consumed — i.e. the body has been read to EOF.

**`resp.Body.Close()` does NOT drain the body.** If unread bytes remain, the transport
cannot tell where the next response begins in the stream, so it discards the connection
instead of reusing it.

The correct pattern when you don't need the response body:

```go
io.ReadAll(resp.Body) // drain remaining bytes to EOF
resp.Body.Close()     // now safe to return connection to pool
```

## Why this code omits the drain

All `POST` endpoints in `cmd/api` respond with `c.SendStatus(fiber.StatusOK)`, which
produces:

```
HTTP/1.1 200 OK
Content-Length: 0
```

With `Content-Length: 0`, the body is immediately at EOF — there are no bytes to drain.
`resp.Body.Close()` alone is sufficient for connection reuse because the transport sees
EOF and returns the connection to the pool.

## Where draining IS done

`apiGet` in `internal/syncer/syncer.go` decodes the response body via:

```go
json.NewDecoder(resp.Body).Decode(dest)
```

`json.Decoder.Decode` reads exactly as much as needed to parse one JSON value, which may
not reach EOF if there is trailing whitespace or a newline. However, the response from
`GET /changes` is a single JSON array with no trailing content, so in practice the
decoder reads to EOF. If this ever changes, an explicit drain should be added before
`Close()`.

## Rule of thumb

| Response has body? | Action before Close()         |
|--------------------|-------------------------------|
| No (`Content-Length: 0`) | Nothing — `Close()` is enough |
| Yes, fully read (decoded/read to EOF) | Nothing — already at EOF      |
| Yes, partially read or ignored | `io.ReadAll(resp.Body)`       |
