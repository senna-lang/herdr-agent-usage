/**
 * Fetches Grok Build's credit usage from the grok.com gRPC-web endpoint.
 * A Bearer token is sometimes sufficient; no browser cookies are required.
 */
package limits

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// GrokBillingEndpoint is the gRPC-web billing method.
const GrokBillingEndpoint = "https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig"

// GrokWebBillingSnapshot is used% + optional reset epoch from the web API.
type GrokWebBillingSnapshot struct {
	UsedPercent          float64
	ResetsAtEpochSeconds *int64
}

type fixed32Field struct {
	path  []int
	value float64
	order int
}

type varintField struct {
	path  []int
	value uint64
}

type protobufScan struct {
	fixed32Fields []fixed32Field
	varintFields  []varintField
}

func readVarint(data []byte, index *int) (uint64, bool) {
	var value uint64
	var shift uint
	for *index < len(data) && shift < 64 {
		b := data[*index]
		*index++
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, true
		}
		shift += 7
	}
	return 0, false
}

func scanProtobuf(data []byte, depth int, path []int, orderStart int) (protobufScan, int) {
	var fixed32Fields []fixed32Field
	var varintFields []varintField
	index := 0
	order := orderStart

	for index < len(data) {
		fieldStart := index
		key, ok := readVarint(data, &index)
		if !ok || key == 0 {
			index = fieldStart + 1
			if index > fieldStart {
				continue
			}
			break
		}
		fieldNumber := int(key >> 3)
		wireType := int(key & 0x07)
		fieldPath := append(append([]int{}, path...), fieldNumber)

		switch wireType {
		case 0:
			value, ok := readVarint(data, &index)
			if !ok {
				index = fieldStart + 1
				continue
			}
			varintFields = append(varintFields, varintField{path: fieldPath, value: value})
		case 1:
			if index+8 > len(data) {
				return protobufScan{fixed32Fields, varintFields}, order
			}
			index += 8
		case 2:
			length, ok := readVarint(data, &index)
			if !ok || index+int(length) > len(data) {
				index = fieldStart + 1
				continue
			}
			start := index
			end := index + int(length)
			if depth < 4 {
				nested, newOrder := scanProtobuf(data[start:end], depth+1, fieldPath, order)
				fixed32Fields = append(fixed32Fields, nested.fixed32Fields...)
				varintFields = append(varintFields, nested.varintFields...)
				order = newOrder
			}
			index = end
		case 5:
			if index+4 > len(data) {
				return protobufScan{fixed32Fields, varintFields}, order
			}
			bits := binary.LittleEndian.Uint32(data[index : index+4])
			value := float64(math.Float32frombits(bits))
			fixed32Fields = append(fixed32Fields, fixed32Field{path: fieldPath, value: value, order: order})
			order++
			index += 4
		default:
			index = fieldStart + 1
		}
	}
	return protobufScan{fixed32Fields, varintFields}, order
}

// GrpcWebDataFrames pulls data payloads out of gRPC-web frames.
func GrpcWebDataFrames(data []byte) [][]byte {
	var frames [][]byte
	index := 0
	for index+5 <= len(data) {
		flags := data[index]
		length := int(data[index+1])<<24 | int(data[index+2])<<16 | int(data[index+3])<<8 | int(data[index+4])
		start := index + 5
		end := start + length
		if length < 0 || end > len(data) {
			return nil
		}
		if flags&0x80 == 0 {
			frames = append(frames, data[start:end])
		}
		index = end
	}
	return frames
}

func looksLikeProtobufPayload(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	first := data[0]
	fieldNumber := first >> 3
	wireType := first & 0x07
	return fieldNumber > 0 && (wireType == 0 || wireType == 1 || wireType == 2 || wireType == 5)
}

// ParseGrokWebBillingResponse reads used% and reset time from the billing response.
func ParseGrokWebBillingResponse(data []byte, nowMs int64) *GrokWebBillingSnapshot {
	payloads := GrpcWebDataFrames(data)
	if len(payloads) == 0 && looksLikeProtobufPayload(data) {
		payloads = [][]byte{data}
	}
	if len(payloads) == 0 {
		return nil
	}

	var scan protobufScan
	order := 0
	for _, payload := range payloads {
		nested, newOrder := scanProtobuf(payload, 0, nil, order)
		scan.fixed32Fields = append(scan.fixed32Fields, nested.fixed32Fields...)
		scan.varintFields = append(scan.varintFields, nested.varintFields...)
		order = newOrder
	}

	var percentCandidates []fixed32Field
	for _, field := range scan.fixed32Fields {
		if len(field.path) == 0 {
			continue
		}
		last := field.path[len(field.path)-1]
		if last == 1 && !math.IsNaN(field.value) && !math.IsInf(field.value, 0) &&
			field.value >= 0 && field.value <= 100 {
			percentCandidates = append(percentCandidates, field)
		}
	}
	// sort: shorter path first, then lower order
	for i := 0; i < len(percentCandidates); i++ {
		for j := i + 1; j < len(percentCandidates); j++ {
			a, b := percentCandidates[i], percentCandidates[j]
			swap := false
			if len(a.path) != len(b.path) {
				swap = len(a.path) > len(b.path)
			} else {
				swap = a.order > b.order
			}
			if swap {
				percentCandidates[i], percentCandidates[j] = percentCandidates[j], percentCandidates[i]
			}
		}
	}
	var parsedPercent *float64
	if len(percentCandidates) > 0 {
		v := percentCandidates[0].value
		parsedPercent = &v
	}

	nowSec := float64(nowMs) / 1000
	type resetCand struct {
		path  []int
		epoch int64
	}
	var resetFields []resetCand
	for _, field := range scan.varintFields {
		if field.value >= 1_700_000_000 && field.value <= 2_100_000_000 {
			epoch := int64(field.value)
			if float64(epoch) > nowSec {
				resetFields = append(resetFields, resetCand{path: field.path, epoch: epoch})
			}
		}
	}

	var preferred *int64
	for _, field := range resetFields {
		if len(field.path) == 3 && field.path[0] == 1 && field.path[1] == 5 && field.path[2] == 1 {
			e := field.epoch
			preferred = &e
			break
		}
	}
	var resetsAtEpochSeconds *int64
	if preferred != nil {
		resetsAtEpochSeconds = preferred
	} else if len(resetFields) > 0 {
		min := resetFields[0].epoch
		for _, f := range resetFields[1:] {
			if f.epoch < min {
				min = f.epoch
			}
		}
		resetsAtEpochSeconds = &min
	}

	hasUsagePeriod := false
	for _, field := range scan.varintFields {
		if len(field.path) >= 2 && field.path[0] == 1 && field.path[1] == 6 {
			hasUsagePeriod = true
			break
		}
		if len(field.path) == 3 && field.path[0] == 1 && field.path[1] == 8 && field.path[2] == 1 &&
			(field.value == 1 || field.value == 2) {
			hasUsagePeriod = true
			break
		}
	}

	noUsageYet := parsedPercent == nil &&
		len(scan.fixed32Fields) == 0 &&
		resetsAtEpochSeconds != nil &&
		hasUsagePeriod

	var usedPercent *float64
	if parsedPercent != nil {
		usedPercent = parsedPercent
	} else if noUsageYet {
		z := 0.0
		usedPercent = &z
	}
	if usedPercent == nil {
		return nil
	}

	return &GrokWebBillingSnapshot{
		UsedPercent:          *usedPercent,
		ResetsAtEpochSeconds: resetsAtEpochSeconds,
	}
}

// LimitWindowFromWebBilling maps a web billing snapshot to LimitWindow.
func LimitWindowFromWebBilling(snap GrokWebBillingSnapshot, nowMs int64) *LimitWindow {
	windowMinutes := 30 * 24 * 60
	if snap.ResetsAtEpochSeconds != nil {
		days := (float64(*snap.ResetsAtEpochSeconds)*1000 - float64(nowMs)) / (24 * 3600_000)
		if days > 0 && days <= 10 {
			windowMinutes = 7 * 24 * 60
		}
	}
	w := &LimitWindow{
		UsedPercentage: snap.UsedPercent,
		WindowMinutes:  &windowMinutes,
	}
	if snap.ResetsAtEpochSeconds != nil {
		r := *snap.ResetsAtEpochSeconds
		w.ResetsAt = &r
	}
	return w
}

// FetchGrokWebBilling POSTs an empty gRPC-web frame with Bearer auth.
func FetchGrokWebBilling(accessToken string, endpoint string) *GrokWebBillingSnapshot {
	if accessToken == "" {
		return nil
	}
	if endpoint == "" {
		endpoint = GrokBillingEndpoint
	}
	client := &http.Client{Timeout: 15 * time.Second}
	// empty gRPC-web data frame: flags=0, length=0
	body := []byte{0, 0, 0, 0, 0}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytesReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Origin", "https://grok.com")
	req.Header.Set("Referer", "https://grok.com/?_s=usage")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("x-grpc-web", "1")
	req.Header.Set("x-user-agent", "connect-es/2.1.1")
	req.Header.Set("User-Agent", "usagebar")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	return ParseGrokWebBillingResponse(data, time.Now().UnixMilli())
}

// FetchGrokSubscriptionTier returns the active subscription tier string.
func FetchGrokSubscriptionTier(accessToken string, endpoint string) *string {
	if accessToken == "" {
		return nil
	}
	if endpoint == "" {
		endpoint = "https://grok.com/rest/subscriptions"
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Origin", "https://grok.com")
	req.Header.Set("Referer", "https://grok.com/?_s=usage")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "usagebar")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	var jsonBody struct {
		Subscriptions []struct {
			Tier   string `json:"tier"`
			Status string `json:"status"`
		} `json:"subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jsonBody); err != nil {
		return nil
	}
	subs := jsonBody.Subscriptions
	if len(subs) == 0 {
		return nil
	}
	active := subs[0]
	for _, s := range subs {
		if strings.Contains(s.Status, "ACTIVE") {
			active = s
			break
		}
	}
	if active.Tier == "" {
		return nil
	}
	t := active.Tier
	return &t
}

// bytesReader wraps a byte slice as io.Reader without extra deps.
type byteSliceReader struct {
	b []byte
	i int
}

func bytesReader(b []byte) *byteSliceReader { return &byteSliceReader{b: b} }

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
