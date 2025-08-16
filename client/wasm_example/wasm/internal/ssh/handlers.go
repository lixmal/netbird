package ssh

import (
	"io"
	"log"
	"syscall/js"

	netbird "github.com/netbirdio/netbird/client/embed"
	"github.com/sirupsen/logrus"
)

// RegisterHandlers registers JavaScript functions for SSH
func RegisterHandlers(nbClient *netbird.Client) {
	js.Global().Set("netbirdSSH", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 2 {
			return js.ValueOf("error: requires host and port")
		}

		host := args[0].String()
		port := args[1].Int()

		username := "root"
		if len(args) > 2 && args[2].String() != "" {
			username = args[2].String()
		}

		promiseConstructor := js.Global().Get("Promise")
		return promiseConstructor.New(js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
			resolve := promiseArgs[0]
			reject := promiseArgs[1]

			go func() {
				client := NewClient(nbClient)

				if err := client.Connect(host, port, username); err != nil {
					reject.Invoke(err.Error())
					return
				}

				if err := client.StartSession(80, 24); err != nil {
					client.Close()
					reject.Invoke(err.Error())
					return
				}

				jsInterface := createJSInterface(client)
				resolve.Invoke(jsInterface)
			}()

			return nil
		}))
	}))

	log.Println("SSH handlers registered for JavaScript")
}

func createJSInterface(client *Client) js.Value {
	jsInterface := js.Global().Get("Object").Call("create", js.Null())

	jsInterface.Set("write", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			return js.ValueOf(false)
		}

		data := args[0]
		var bytes []byte

		if data.Type() == js.TypeString {
			bytes = []byte(data.String())
		} else {
			uint8Array := js.Global().Get("Uint8Array").New(data)
			length := uint8Array.Get("length").Int()
			bytes = make([]byte, length)
			js.CopyBytesToGo(bytes, uint8Array)
		}

		_, err := client.Write(bytes)
		return js.ValueOf(err == nil)
	}))

	jsInterface.Set("resize", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 2 {
			return js.ValueOf(false)
		}
		cols := args[0].Int()
		rows := args[1].Int()
		err := client.Resize(cols, rows)
		return js.ValueOf(err == nil)
	}))

	jsInterface.Set("close", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		client.Close()
		return js.Undefined()
	}))

	go readLoop(client, jsInterface)

	return jsInterface
}

func readLoop(client *Client, jsInterface js.Value) {
	buffer := make([]byte, 4096)
	for {
		n, err := client.Read(buffer)
		if err != nil {
			if err != io.EOF {
				logrus.Debugf("SSH read error: %v", err)
			}
			if onclose := jsInterface.Get("onclose"); !onclose.IsUndefined() {
				onclose.Invoke()
			}
			client.Close()
			return
		}

		if ondata := jsInterface.Get("ondata"); !ondata.IsUndefined() {
			uint8Array := js.Global().Get("Uint8Array").New(n)
			js.CopyBytesToJS(uint8Array, buffer[:n])
			ondata.Invoke(uint8Array)
		}
	}
}
