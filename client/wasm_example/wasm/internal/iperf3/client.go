package iperf3

import (
	"context"
	"fmt"
	"net"
	"sync"
	"syscall/js"
	"time"

	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/client"
)

// Client represents an iperf3 client
type Client struct {
	conn      net.Conn
	mu        sync.Mutex
	isRunning bool
	ctx       context.Context
	cancel    context.CancelFunc

	host      string
	port      int
	duration  int
	reverse   bool
	parallel  int
	bandwidth int64
	protocol  string

	bytesSent     int64
	bytesReceived int64
	startTime     time.Time
	endTime       time.Time
	intervals     []IntervalStats

	onProgress js.Value
	onComplete js.Value
	onError    js.Value
}

// IntervalStats holds statistics for a time interval
type IntervalStats struct {
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Bytes       int64   `json:"bytes"`
	BitsPerSec  float64 `json:"bits_per_second"`
	Jitter      float64 `json:"jitter_ms,omitempty"`
	LostPackets int     `json:"lost_packets,omitempty"`
	Packets     int     `json:"packets,omitempty"`
}

// TestParameters defines test configuration
type TestParameters struct {
	Protocol   string `json:"protocol"`
	Duration   int    `json:"duration"`
	NumStreams int    `json:"num_streams"`
	BlkSize    int    `json:"blksize"`
	Reverse    bool   `json:"reverse"`
	Bandwidth  int64  `json:"target_bandwidth"`
	Cookie     string `json:"cookie"`
}

// TestResults contains test results
type TestResults struct {
	StartTime     time.Time       `json:"start_time"`
	EndTime       time.Time       `json:"end_time"`
	BytesSent     int64           `json:"bytes_sent"`
	BytesReceived int64           `json:"bytes_received"`
	Duration      float64         `json:"duration"`
	Intervals     []IntervalStats `json:"intervals"`
}

// NewClient creates a new iperf3 client
func NewClient() *Client {
	return &Client{
		duration:  DEFAULT_TEST_DURATION,
		parallel:  DEFAULT_PARALLEL,
		protocol:  "tcp",
		bandwidth: DEFAULT_BANDWIDTH,
	}
}

// Connect establishes connection to iperf3 server
func (c *Client) Connect(host string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		return fmt.Errorf("test already running")
	}

	c.host = host
	c.port = port

	addr := fmt.Sprintf("%s:%d", host, port)
	nbClient := client.GetClient()
	if nbClient == nil {
		return fmt.Errorf("NetBird client not initialized")
	}

	conn, err := nbClient.Dial(context.Background(), c.protocol, addr)
	if err != nil {
		return fmt.Errorf("connect to iperf3 server: %w", err)
	}

	c.conn = conn
	c.ctx, c.cancel = context.WithCancel(context.Background())

	if err := c.sendCookie(); err != nil {
		conn.Close()
		return fmt.Errorf("send cookie: %w", err)
	}

	return nil
}

// RunTest executes the iperf3 test
func (c *Client) RunTest() error {
	c.mu.Lock()
	if c.isRunning {
		c.mu.Unlock()
		return fmt.Errorf("test already running")
	}
	c.isRunning = true
	c.startTime = time.Now()
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.isRunning = false
		c.endTime = time.Now()
		c.mu.Unlock()
	}()

	params := TestParameters{
		Protocol:   c.protocol,
		Duration:   c.duration,
		NumStreams: c.parallel,
		BlkSize:    DEFAULT_BLOCK_SIZE,
		Reverse:    c.reverse,
		Bandwidth:  c.bandwidth,
		Cookie:     IPERF3_COOKIE,
	}

	if err := c.sendControlMessage(PARAM_EXCHANGE, params); err != nil {
		return fmt.Errorf("send parameters: %w", err)
	}

	msgType, _, err := c.readControlMessage()
	if err != nil {
		return fmt.Errorf("read server response: %w", err)
	}

	if msgType == ACCESS_DENIED || msgType == SERVER_ERROR {
		return fmt.Errorf("server rejected test")
	}

	if err := c.sendControlMessage(CREATE_STREAMS, nil); err != nil {
		return fmt.Errorf("create streams: %w", err)
	}

	if c.reverse {
		return c.runReverseTest()
	}
	return c.runForwardTest()
}

// Stop cancels the running test
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.conn != nil {
		c.sendControlMessage(CLIENT_TERMINATE, nil)
		c.conn.Close()
		c.conn = nil
	}

	c.isRunning = false
}
