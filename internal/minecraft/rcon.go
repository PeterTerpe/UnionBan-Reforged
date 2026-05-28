package minecraft

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	rconPacketAuth    int32 = 3
	rconPacketCommand int32 = 2
)

type RCONClient struct {
	address  string
	password string
	timeout  time.Duration

	mu     sync.Mutex
	conn   net.Conn
	nextID int32
}

func NewRCONClient(address string, password string, timeout time.Duration) *RCONClient {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &RCONClient{
		address:  address,
		password: password,
		timeout:  timeout,
		nextID:   1,
	}
}

func (c *RCONClient) Command(ctx context.Context, command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		if err := c.connectLocked(ctx); err != nil {
			return "", err
		}

		response, err := c.commandLocked(ctx, command)
		if err == nil {
			return response, nil
		}

		lastErr = err
		c.closeLocked()
	}

	return "", lastErr
}

func (c *RCONClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closeLocked()
}

func (c *RCONClient) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}

	dialer := net.Dialer{
		Timeout: c.timeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return err
	}

	c.conn = conn

	requestID := c.nextRequestIDLocked()
	if err := c.writePacketLocked(ctx, requestID, rconPacketAuth, c.password); err != nil {
		c.closeLocked()
		return err
	}

	responseID, _, _, err := c.readPacketLocked(ctx)
	if err != nil {
		c.closeLocked()
		return err
	}

	if responseID == -1 {
		c.closeLocked()
		return errors.New("RCON authentication failed")
	}

	return nil
}

func (c *RCONClient) commandLocked(ctx context.Context, command string) (string, error) {
	requestID := c.nextRequestIDLocked()

	if err := c.writePacketLocked(ctx, requestID, rconPacketCommand, command); err != nil {
		return "", err
	}

	responseID, _, payload, err := c.readPacketLocked(ctx)
	if err != nil {
		return "", err
	}

	if responseID != requestID {
		return "", fmt.Errorf("unexpected RCON response id %d for request %d", responseID, requestID)
	}

	return payload, nil
}

func (c *RCONClient) writePacketLocked(ctx context.Context, requestID int32, packetType int32, payload string) error {
	if err := c.setDeadlineLocked(ctx); err != nil {
		return err
	}

	payloadBytes := []byte(payload)
	packetLength := int32(4 + 4 + len(payloadBytes) + 2)

	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.LittleEndian, packetLength); err != nil {
		return err
	}

	if err := binary.Write(&buffer, binary.LittleEndian, requestID); err != nil {
		return err
	}

	if err := binary.Write(&buffer, binary.LittleEndian, packetType); err != nil {
		return err
	}

	buffer.Write(payloadBytes)
	buffer.Write([]byte{0, 0})

	_, err := c.conn.Write(buffer.Bytes())
	return err
}

func (c *RCONClient) readPacketLocked(ctx context.Context) (int32, int32, string, error) {
	if err := c.setDeadlineLocked(ctx); err != nil {
		return 0, 0, "", err
	}

	var packetLength int32
	if err := binary.Read(c.conn, binary.LittleEndian, &packetLength); err != nil {
		return 0, 0, "", err
	}

	if packetLength < 10 || packetLength > 1024*1024 {
		return 0, 0, "", fmt.Errorf("invalid RCON packet length %d", packetLength)
	}

	body := make([]byte, packetLength)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return 0, 0, "", err
	}

	requestID := int32(binary.LittleEndian.Uint32(body[0:4]))
	packetType := int32(binary.LittleEndian.Uint32(body[4:8]))
	payloadBytes := body[8 : len(body)-2]

	return requestID, packetType, string(payloadBytes), nil
}

func (c *RCONClient) setDeadlineLocked(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}

	return c.conn.SetDeadline(deadline)
}

func (c *RCONClient) nextRequestIDLocked() int32 {
	requestID := c.nextID
	c.nextID++

	if c.nextID < 1 {
		c.nextID = 1
	}

	return requestID
}

func (c *RCONClient) closeLocked() error {
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil

	return err
}
