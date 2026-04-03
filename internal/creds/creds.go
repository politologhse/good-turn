// Package creds fetches TURN credentials from VK and Yandex APIs.
package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// LogFunc is a callback for log messages.
type LogFunc func(string)

// GetCredsFunc fetches TURN credentials: username, password, address.
type GetCredsFunc func(string) (string, string, string, error)

// WithRetry wraps a GetCredsFunc with exponential backoff retry.
func WithRetry(f GetCredsFunc, maxAttempts int, logf LogFunc) GetCredsFunc {
	return func(link string) (string, string, string, error) {
		var lastErr error
		backoff := 2 * time.Second
		for i := 0; i < maxAttempts; i++ {
			user, pass, addr, err := f(link)
			if err == nil {
				return user, pass, addr, nil
			}
			lastErr = err
			if i < maxAttempts-1 {
				logf(fmt.Sprintf("Creds attempt %d/%d failed: %s, retrying in %s...", i+1, maxAttempts, err, backoff))
				time.Sleep(backoff)
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
		return "", "", "", fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
	}
}

// ParseTurnURL extracts host:port from a TURN URL like "turn:1.2.3.4:3478?transport=udp".
func ParseTurnURL(raw string) string {
	clean := strings.Split(raw, "?")[0]
	return strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
}

// getString safely extracts a nested string from a JSON map.
func getString(m map[string]interface{}, keys ...string) (string, error) {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("expected object at key %q", k)
		}
		cur = mm[k]
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("expected string at %v, got %T", keys, cur)
	}
	return s, nil
}

func doHTTPPost(data, url, userAgent string, transport http.RoundTripper) (map[string]interface{}, error) {
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", userAgent)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0"

// GetVkCreds fetches TURN credentials from VK call link.
func GetVkCreds(link string, dialer *dnsdialer.Dialer, logf LogFunc) (string, string, string, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialer.DialContext,
	}

	// Step 1: get anonymous token
	resp, err := doHTTPPost(
		"client_id=6287487&token_type=messages&client_secret=QbYic1K3lEV5kTGiqlq2&version=1&app_id=6287487",
		"https://login.vk.ru/?act=get_anonym_token",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("VK anon token request: %w", err)
	}
	token1, err := getString(resp, "data", "access_token")
	if err != nil {
		return "", "", "", fmt.Errorf("VK anon token parse: %w", err)
	}

	// Step 2: get call token
	resp, err = doHTTPPost(
		fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=123&access_token=%s", link, token1),
		"https://api.vk.ru/method/calls.getAnonymousToken?v=5.274&client_id=6287487",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("VK call token request: %w", err)
	}
	token2, err := getString(resp, "response", "token")
	if err != nil {
		return "", "", "", fmt.Errorf("VK call token parse: %w", err)
	}

	// Step 3: OK session
	resp, err = doHTTPPost(
		fmt.Sprintf("session_data=%%7B%%22version%%22%%3A2%%2C%%22device_id%%22%%3A%%22%s%%22%%2C%%22client_version%%22%%3A1.1%%2C%%22client_type%%22%%3A%%22SDK_JS%%22%%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", uuid.New()),
		"https://calls.okcdn.ru/fb.do",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("OK session request: %w", err)
	}
	token3, err := getString(resp, "session_key")
	if err != nil {
		return "", "", "", fmt.Errorf("OK session parse: %w", err)
	}

	// Step 4: join conference, get TURN creds
	resp, err = doHTTPPost(
		fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3),
		"https://calls.okcdn.ru/fb.do",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("TURN creds request: %w", err)
	}

	user, err := getString(resp, "turn_server", "username")
	if err != nil {
		return "", "", "", fmt.Errorf("TURN username: %w", err)
	}
	pass, err := getString(resp, "turn_server", "credential")
	if err != nil {
		return "", "", "", fmt.Errorf("TURN credential: %w", err)
	}

	turnServer, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", "", fmt.Errorf("missing turn_server in response")
	}
	urls, ok := turnServer["urls"].([]interface{})
	if !ok || len(urls) == 0 {
		return "", "", "", fmt.Errorf("missing turn_server urls")
	}
	turnURL, ok := urls[0].(string)
	if !ok {
		return "", "", "", fmt.Errorf("turn_server url is not a string")
	}

	return user, pass, ParseTurnURL(turnURL), nil
}

// GetYandexCreds fetches TURN credentials from Yandex Telemost link.
func GetYandexCreds(link string, logf LogFunc) (string, string, string, error) {
	const telemostConfHost = "cloud-api.yandex.ru"
	telemostConfPath := fmt.Sprintf("/telemost_front/v2/telemost/conferences/https%%3A%%2F%%2Ftelemost.yandex.ru%%2Fj%%2F%s/connection?next_gen_media_platform_allowed=false", link)

	type ConferenceResponse struct {
		RoomID string `json:"room_id"`
		PeerID string `json:"peer_id"`
		ClientConfiguration struct {
			MediaServerURL string `json:"media_server_url"`
		} `json:"client_configuration"`
		Credentials string `json:"credentials"`
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequest("GET", "https://"+telemostConfHost+telemostConfPath, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://telemost.yandex.ru/")
	req.Header.Set("Origin", "https://telemost.yandex.ru")
	req.Header.Set("Client-Instance-Id", uuid.New().String())

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("telemost: status=%s body=%s", resp.Status, string(body))
	}

	var conf ConferenceResponse
	if err := json.NewDecoder(resp.Body).Decode(&conf); err != nil {
		return "", "", "", fmt.Errorf("decode conf: %w", err)
	}

	h := http.Header{}
	h.Set("Origin", "https://telemost.yandex.ru")
	h.Set("User-Agent", defaultUserAgent)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsDialer := websocket.Dialer{}
	conn, _, err := wsDialer.DialContext(ctx, conf.ClientConfiguration.MediaServerURL, h)
	if err != nil {
		return "", "", "", fmt.Errorf("ws dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	hello := buildYandexHello(conf.PeerID, conf.RoomID, conf.Credentials)
	if err := conn.WriteJSON(hello); err != nil {
		return "", "", "", fmt.Errorf("ws write: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return "", "", "", fmt.Errorf("ws deadline: %w", err)
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return "", "", "", fmt.Errorf("ws read: %w", err)
		}

		var ack struct {
			Ack struct {
				Status struct{ Code string } `json:"status"`
			} `json:"ack"`
		}
		if err := json.Unmarshal(msg, &ack); err == nil && ack.Ack.Status.Code != "" {
			continue
		}

		var wssResp struct {
			ServerHello struct {
				RtcConfiguration struct {
					IceServers []struct {
						Urls       []string `json:"urls"`
						Username   string   `json:"username,omitempty"`
						Credential string   `json:"credential,omitempty"`
					} `json:"iceServers"`
				} `json:"rtcConfiguration"`
			} `json:"serverHello"`
		}
		if err := json.Unmarshal(msg, &wssResp); err == nil {
			for _, s := range wssResp.ServerHello.RtcConfiguration.IceServers {
				for _, u := range s.Urls {
					if !strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
						continue
					}
					if strings.Contains(u, "transport=tcp") {
						continue
					}
					return s.Username, s.Credential, ParseTurnURL(u), nil
				}
			}
		}
	}
}

func buildYandexHello(participantID, roomID, credentials string) map[string]interface{} {
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"hello": map[string]interface{}{
			"participantMeta":        map[string]interface{}{"name": "Guest", "role": "SPEAKER", "description": "", "sendAudio": false, "sendVideo": false},
			"participantAttributes":  map[string]interface{}{"name": "Guest", "role": "SPEAKER", "description": ""},
			"sendAudio": false, "sendVideo": false, "sendSharing": false,
			"participantId": participantID, "roomId": roomID,
			"serviceName": "telemost", "credentials": credentials,
			"sdkInfo": map[string]interface{}{
				"implementation": "browser", "version": "5.15.0",
				"userAgent": defaultUserAgent, "hwConcurrency": 4,
			},
			"sdkInitializationId":    uuid.New().String(),
			"disablePublisher":       false,
			"disableSubscriber":      false,
			"disableSubscriberAudio": false,
			"capabilitiesOffer": map[string]interface{}{
				"offerAnswerMode": []string{"SEPARATE"}, "initialSubscriberOffer": []string{"ON_HELLO"},
				"slotsMode": []string{"FROM_CONTROLLER"}, "simulcastMode": []string{"DISABLED"},
				"selfVadStatus": []string{"FROM_SERVER"}, "dataChannelSharing": []string{"TO_RTP"},
				"videoEncoderConfig": []string{"NO_CONFIG"}, "dataChannelVideoCodec": []string{"VP8"},
				"bandwidthLimitationReason": []string{"BANDWIDTH_REASON_DISABLED"},
				"sdkDefaultDeviceManagement": []string{"SDK_DEFAULT_DEVICE_MANAGEMENT_DISABLED"},
				"joinOrderLayout": []string{"JOIN_ORDER_LAYOUT_DISABLED"}, "pinLayout": []string{"PIN_LAYOUT_DISABLED"},
				"sendSelfViewVideoSlot": []string{"SEND_SELF_VIEW_VIDEO_SLOT_DISABLED"},
				"serverLayoutTransition": []string{"SERVER_LAYOUT_TRANSITION_DISABLED"},
				"sdkPublisherOptimizeBitrate": []string{"SDK_PUBLISHER_OPTIMIZE_BITRATE_DISABLED"},
				"sdkNetworkLostDetection": []string{"SDK_NETWORK_LOST_DETECTION_DISABLED"},
				"sdkNetworkPathMonitor": []string{"SDK_NETWORK_PATH_MONITOR_DISABLED"},
				"publisherVp9": []string{"PUBLISH_VP9_DISABLED"}, "svcMode": []string{"SVC_MODE_DISABLED"},
				"subscriberOfferAsyncAck": []string{"SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED"},
				"svcModes": []string{"FALSE"}, "reportTelemetryModes": []string{"TRUE"},
				"keepDefaultDevicesModes": []string{"TRUE"},
			},
		},
	}
}
