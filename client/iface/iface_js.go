package iface

import (
	"github.com/netbirdio/netbird/client/iface/bind"
	"github.com/netbirdio/netbird/client/iface/device"
	"github.com/netbirdio/netbird/client/iface/netstack"
	"github.com/netbirdio/netbird/client/iface/wgaddr"
	"github.com/netbirdio/netbird/client/iface/wgproxy"
)

// NewWGIFace creates a new WireGuard interface for WASM (always uses netstack mode)
func NewWGIFace(opts WGIFaceOpts) (*WGIface, error) {
	wgAddress, err := wgaddr.ParseWGAddress(opts.Address)
	if err != nil {
		return nil, err
	}

	// WASM always uses netstack mode
	iceBind := bind.NewICEBind(opts.TransportNet, opts.FilterFn, wgAddress)

	wgIface := &WGIface{
		tun:            device.NewNetstackDevice(opts.IFaceName, wgAddress, opts.WGPort, opts.WGPrivKey, opts.MTU, iceBind, netstack.ListenAddr()),
		userspaceBind:  true,
		wgProxyFactory: wgproxy.NewUSPFactory(iceBind),
	}

	return wgIface, nil
}
