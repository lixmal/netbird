package iperf3

import (
	"log"
	"syscall/js"
)

var iperf3Client *Client

// RegisterHandlers registers JavaScript handlers for iperf3 testing
func RegisterHandlers() {
	js.Global().Set("createIperf3Client", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Println("Creating iPerf3 client...")
		iperf3Client = NewClient()
		return nil
	}))
	
	js.Global().Set("setIperf3Callbacks", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 3 || iperf3Client == nil {
			return "Invalid arguments or client not created"
		}
		
		iperf3Client.onProgress = args[0]
		iperf3Client.onComplete = args[1]
		iperf3Client.onError = args[2]
		return nil
	}))
	
	js.Global().Set("configureIperf3Test", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 || iperf3Client == nil {
			return "Invalid arguments or client not created"
		}
		
		config := args[0]
		if duration := config.Get("duration"); !duration.IsUndefined() {
			iperf3Client.duration = duration.Int()
		}
		if reverse := config.Get("reverse"); !reverse.IsUndefined() {
			iperf3Client.reverse = reverse.Bool()
		}
		if parallel := config.Get("parallel"); !parallel.IsUndefined() {
			iperf3Client.parallel = parallel.Int()
		}
		if bandwidth := config.Get("bandwidth"); !bandwidth.IsUndefined() {
			iperf3Client.bandwidth = int64(bandwidth.Int())
		}
		if protocol := config.Get("protocol"); !protocol.IsUndefined() {
			iperf3Client.protocol = protocol.String()
		}
		return nil
	}))
	
	js.Global().Set("startIperf3Test", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 2 || iperf3Client == nil {
			return "Invalid arguments or client not created"
		}
		
		host := args[0].String()
		port := args[1].Int()
		
		go func() {
			log.Printf("Connecting to iperf3 server at %s:%d", host, port)
			if err := iperf3Client.Connect(host, port); err != nil {
				log.Printf("Connection failed: %v", err)
				iperf3Client.reportError(err.Error())
				return
			}
			
			log.Println("Running iperf3 test...")
			if err := iperf3Client.RunTest(); err != nil {
				log.Printf("Test failed: %v", err)
				iperf3Client.reportError(err.Error())
				return
			}
			
			log.Println("Test completed")
			iperf3Client.reportComplete()
		}()
		
		return nil
	}))
	
	js.Global().Set("stopIperf3Test", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if iperf3Client != nil {
			iperf3Client.Stop()
		}
		return nil
	}))
	
	log.Println("iperf3 handlers registered for JavaScript")
}


func (c *Client) reportProgress(stats IntervalStats) {
	if !c.onProgress.IsUndefined() && !c.onProgress.IsNull() {
		jsStats := js.ValueOf(map[string]interface{}{
			"start":           stats.Start,
			"end":             stats.End,
			"bytes":           stats.Bytes,
			"bits_per_second": stats.BitsPerSec,
		})
		c.onProgress.Invoke(jsStats)
	}
}

func (c *Client) reportComplete() {
	if !c.onComplete.IsUndefined() && !c.onComplete.IsNull() {
		results := c.getResults()
		c.onComplete.Invoke(results)
	}
}

func (c *Client) reportError(errMsg string) {
	if !c.onError.IsUndefined() && !c.onError.IsNull() {
		c.onError.Invoke(errMsg)
	}
}

func (c *Client) getResults() js.Value {
	duration := c.endTime.Sub(c.startTime).Seconds()
	throughput := float64(c.bytesSent+c.bytesReceived) * 8 / duration

	intervals := make([]interface{}, len(c.intervals))
	for i, interval := range c.intervals {
		intervals[i] = map[string]interface{}{
			"start":           interval.Start,
			"end":             interval.End,
			"bytes":           interval.Bytes,
			"bits_per_second": interval.BitsPerSec,
		}
	}

	return js.ValueOf(map[string]interface{}{
		"start_time":     c.startTime.Unix(),
		"end_time":       c.endTime.Unix(),
		"bytes_sent":     c.bytesSent,
		"bytes_received": c.bytesReceived,
		"duration":       duration,
		"throughput":     throughput,
		"intervals":      intervals,
	})
}
