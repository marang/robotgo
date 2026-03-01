# Mock Wayland Server

`mock_server.c` contains a minimal Wayland compositor used during tests to exercise screencopy and dmabuf code paths.

If the Wayland protocol definitions change, regenerate the headers with [wayland-scanner](https://wayland.freedesktop.org/wayland-scanner.html).
From the repository root run:

```
go run wayland_generate.go
```

The mock server will be rebuilt automatically the next time `go test` is executed.
