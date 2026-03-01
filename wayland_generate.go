//go:build ignore
// +build ignore

package main

//go:generate curl -L https://raw.githubusercontent.com/swaywm/wlroots/master/protocol/virtual-keyboard-unstable-v1.xml -o virtual-keyboard-unstable-v1.xml
//go:generate sh -c "wayland-scanner client-header virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.h && wayland-scanner private-code virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.c && sed -i '1i//go:build linux && wayland\\n// +build linux,wayland' virtual-keyboard-unstable-v1-client-protocol.c && rm virtual-keyboard-unstable-v1.xml"

func main() {}
