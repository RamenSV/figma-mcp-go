package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

var followerLogger = log.New(os.Stderr, "[follower] ", 0)

// Follower proxies MCP tool calls to the leader via HTTP /rpc.
type Follower struct {
	leaderURL string
	client    *http.Client
}

// NewFollower creates a Follower pointed at the given leader base URL.
func NewFollower(leaderURL string) *Follower {
	return &Follower{
		leaderURL: leaderURL,
		client: &http.Client{
			// The leader's bridge timeout starts at 30s but progress updates extend it,
			// so long create_nodes calls can run for minutes. The leader times out first.
			Timeout: 10 * time.Minute,
		},
	}
}

// Send proxies a tool call to the leader.
func (f *Follower) Send(ctx context.Context, tool string, nodeIDs []string, params map[string]interface{}) (BridgeResponse, error) {
	followerLogger.Printf("proxy %s nodeIDs=%v params=%v → %s/rpc", tool, nodeIDs, params, f.leaderURL)
	start := time.Now()

	rpcReq := RPCRequest{
		Tool:    tool,
		NodeIDs: nodeIDs,
		Params:  params,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return BridgeResponse{}, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.leaderURL+"/rpc", bytes.NewReader(body))
	if err != nil {
		return BridgeResponse{}, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		followerLogger.Printf("proxy %s rpc error: %v", tool, err)
		return BridgeResponse{}, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return BridgeResponse{}, fmt.Errorf("read response: %w", err)
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return BridgeResponse{}, fmt.Errorf("unmarshal: %w", err)
	}

	if rpcResp.Error != "" {
		followerLogger.Printf("proxy %s error from leader in %dms: %s", tool, time.Since(start).Milliseconds(), rpcResp.Error)
		return BridgeResponse{Error: rpcResp.Error}, nil
	}

	followerLogger.Printf("proxy %s ok in %dms", tool, time.Since(start).Milliseconds())
	return BridgeResponse{
		Type: tool,
		Data: rpcResp.Data,
	}, nil
}

// Ping checks if the leader is alive. Returns true if healthy.
func (f *Follower) Ping(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.leaderURL+"/ping", nil)
	if err != nil {
		followerLogger.Printf("ping new request error: %v", err)
		return false
	}

	resp, err := f.client.Do(req)
	if err != nil {
		followerLogger.Printf("ping %s failed: %v", f.leaderURL, err)
		return false
	}
	resp.Body.Close()
	ok := resp.StatusCode == http.StatusOK
	followerLogger.Printf("ping %s → %d (healthy=%v)", f.leaderURL, resp.StatusCode, ok)
	return ok
}
