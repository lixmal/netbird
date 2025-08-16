package iperf3

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	IPERF3_COOKIE_SIZE = 37
	IPERF3_COOKIE      = "d0a224c1a134b387b9e39c71efa2e80600000000"

	TEST_START       = 1
	TEST_RUNNING     = 2
	RESULT_REQUEST   = 3
	TEST_END         = 4
	STREAM_BEGIN     = 5
	STREAM_RUNNING   = 6
	STREAM_END       = 7
	ALL_STREAMS_END  = 8
	PARAM_EXCHANGE   = 9
	CREATE_STREAMS   = 10
	SERVER_TERMINATE = 11
	CLIENT_TERMINATE = 12
	EXCHANGE_RESULTS = 13
	DISPLAY_RESULTS  = 14
	IPERF_START      = 15
	IPERF_DONE       = 16
	ACCESS_DENIED    = 0xFF
	SERVER_ERROR     = 0xFE

	DEFAULT_TEST_DURATION = 10
	DEFAULT_BLOCK_SIZE    = 128 * 1024
	DEFAULT_PARALLEL      = 1
	DEFAULT_BANDWIDTH     = 0
)

func (c *Client) sendCookie() error {
	cookie := []byte(IPERF3_COOKIE)[:IPERF3_COOKIE_SIZE]
	_, err := c.conn.Write(cookie)
	return err
}

func (c *Client) sendControlMessage(msgType byte, params interface{}) error {
	msg := make([]byte, 5)
	msg[0] = msgType

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		binary.BigEndian.PutUint32(msg[1:5], uint32(len(data)))
		msg = append(msg, data...)
	} else {
		binary.BigEndian.PutUint32(msg[1:5], 0)
	}

	_, err := c.conn.Write(msg)
	return err
}

func (c *Client) readControlMessage() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}

	msgType := header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	if length == 0 {
		return msgType, nil, nil
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return 0, nil, fmt.Errorf("read data: %w", err)
	}

	return msgType, data, nil
}

func (c *Client) runForwardTest() error {
	if err := c.sendControlMessage(TEST_START, nil); err != nil {
		return fmt.Errorf("send test start: %w", err)
	}

	testData := make([]byte, DEFAULT_BLOCK_SIZE)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	intervalStart := time.Now()
	intervalBytes := int64(0)
	lastReport := time.Now()

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
			elapsed := time.Since(c.startTime)
			if elapsed >= time.Duration(c.duration)*time.Second {
				if err := c.sendControlMessage(TEST_END, nil); err != nil {
					return fmt.Errorf("send test end: %w", err)
				}
				return c.finishTest()
			}

			n, err := c.conn.Write(testData)
			if err != nil {
				c.reportError(fmt.Sprintf("Write error: %v", err))
				return err
			}

			c.bytesSent += int64(n)
			intervalBytes += int64(n)

			if time.Since(lastReport) >= time.Second {
				c.reportInterval(intervalStart, intervalBytes)
				intervalStart = time.Now()
				intervalBytes = 0
				lastReport = time.Now()
			}
		}
	}
}

func (c *Client) runReverseTest() error {
	if err := c.sendControlMessage(TEST_START, nil); err != nil {
		return fmt.Errorf("send test start: %w", err)
	}

	buffer := make([]byte, DEFAULT_BLOCK_SIZE)
	intervalStart := time.Now()
	intervalBytes := int64(0)
	lastReport := time.Now()

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
			elapsed := time.Since(c.startTime)
			if elapsed >= time.Duration(c.duration)*time.Second {
				if err := c.sendControlMessage(TEST_END, nil); err != nil {
					return fmt.Errorf("send test end: %w", err)
				}
				return c.finishTest()
			}

			c.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, err := c.conn.Read(buffer)
			if err != nil {
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					continue
				}
				c.reportError(fmt.Sprintf("Read error: %v", err))
				return err
			}

			c.bytesReceived += int64(n)
			intervalBytes += int64(n)

			if time.Since(lastReport) >= time.Second {
				c.reportInterval(intervalStart, intervalBytes)
				intervalStart = time.Now()
				intervalBytes = 0
				lastReport = time.Now()
			}
		}
	}
}

func (c *Client) finishTest() error {
	if err := c.sendControlMessage(EXCHANGE_RESULTS, nil); err != nil {
		return fmt.Errorf("exchange results: %w", err)
	}

	msgType, data, err := c.readControlMessage()
	if err != nil {
		return fmt.Errorf("read results: %w", err)
	}

	if msgType == EXCHANGE_RESULTS && data != nil {
		var serverResults TestResults
		if err := json.Unmarshal(data, &serverResults); err == nil {
			c.intervals = append(c.intervals, serverResults.Intervals...)
		}
	}

	if err := c.sendControlMessage(IPERF_DONE, nil); err != nil {
		return fmt.Errorf("send done: %w", err)
	}

	c.reportComplete()
	return nil
}

func (c *Client) reportInterval(start time.Time, bytes int64) {
	elapsed := time.Since(start).Seconds()
	bitsPerSec := float64(bytes*8) / elapsed

	stats := IntervalStats{
		Start:      start.Sub(c.startTime).Seconds(),
		End:        time.Now().Sub(c.startTime).Seconds(),
		Bytes:      bytes,
		BitsPerSec: bitsPerSec,
	}

	c.intervals = append(c.intervals, stats)
	c.reportProgress(stats)
}
