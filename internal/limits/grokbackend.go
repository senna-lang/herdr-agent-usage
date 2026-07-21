/**
 * Grok custom-model backend detection.
 *
 * Grok sessions record modelId but not the inference host. Custom models are
 * declared in ~/.grok/config.toml as [model.<id>] with base_url; joining the
 * two yields a real backend label (openai, ollama, anthropic, …).
 *
 * Pure detectors only; file reads live in billingmode_io.go.
 */
package limits

import (
	"encoding/json"
	"strings"
)

// Default Grok backend when the model has no custom base_url override.
const grokDefaultBackendID = "xai"

// GrokModelConfig is the subset of [model.*] we need for backend labelling.
type GrokModelConfig struct {
	// BaseURL is the inference endpoint; empty means inherit xAI default.
	BaseURL string
	// Model is the API model id (optional; section name is the lookup key).
	Model string
}

// ParseGrokModelConfigs extracts [model.<name>] tables from a config.toml body.
// Only base_url / model string fields are read — enough for backend labels
// without pulling a full TOML dependency.
func ParseGrokModelConfigs(tomlBody string) map[string]GrokModelConfig {
	out := map[string]GrokModelConfig{}
	var section string
	var cur GrokModelConfig
	flush := func() {
		if section == "" {
			return
		}
		out[section] = cur
		section = ""
		cur = GrokModelConfig{}
	}
	for _, line := range strings.Split(tomlBody, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			flush()
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if strings.HasPrefix(name, "model.") {
				section = strings.TrimSpace(name[len("model."):])
				// Strip optional quotes around the key.
				section = strings.Trim(section, `"'`)
			}
			continue
		}
		if section == "" {
			continue
		}
		key, val, ok := splitTomlKV(trimmed)
		if !ok {
			continue
		}
		switch key {
		case "base_url":
			cur.BaseURL = val
		case "model":
			cur.Model = val
		}
	}
	flush()
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseGrokModelsBaseURL reads [endpoints] models_base_url or top-level
// models_base_url from config.toml (global inference host override).
func ParseGrokModelsBaseURL(tomlBody string) string {
	section := ""
	for _, line := range strings.Split(tomlBody, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		key, val, ok := splitTomlKV(trimmed)
		if !ok {
			continue
		}
		if key == "models_base_url" && (section == "endpoints" || section == "") {
			return val
		}
	}
	return ""
}

func splitTomlKV(line string) (key, val string, ok bool) {
	// Strip inline comments carefully: only when # is outside quotes.
	line = stripTomlComment(line)
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	if key == "" {
		return "", "", false
	}
	// Quoted string.
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}

func stripTomlComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

// GrokBackendIDForModel joins a session model id with config-declared models.
// modelsBaseURL is the optional global [endpoints].models_base_url override.
// Empty modelID with no global override yields "" (unknown).
func GrokBackendIDForModel(modelID string, models map[string]GrokModelConfig, modelsBaseURL string) string {
	if models != nil {
		if cfg, ok := models[modelID]; ok {
			if cfg.BaseURL != "" {
				if id := BackendIDFromBaseURL(cfg.BaseURL); id != "" {
					return id
				}
			}
			// Declared custom model without base_url: still not necessarily xAI
			// if a global models endpoint is set.
			if modelsBaseURL != "" {
				if id := BackendIDFromBaseURL(modelsBaseURL); id != "" {
					return id
				}
			}
			// Known section, no URL → built-in override (api_key only) = xAI.
			return grokDefaultBackendID
		}
		// Also match by the config's model= field when the header name differs.
		for name, cfg := range models {
			if cfg.Model != "" && cfg.Model == modelID {
				if cfg.BaseURL != "" {
					if id := BackendIDFromBaseURL(cfg.BaseURL); id != "" {
						return id
					}
				}
				if modelsBaseURL != "" {
					if id := BackendIDFromBaseURL(modelsBaseURL); id != "" {
						return id
					}
				}
				return grokDefaultBackendID
			}
			_ = name
		}
	}
	if modelsBaseURL != "" {
		if id := BackendIDFromBaseURL(modelsBaseURL); id != "" {
			return id
		}
	}
	if modelID == "" {
		return ""
	}
	// Built-in catalog model with no config entry → xAI.
	return grokDefaultBackendID
}

// GrokBillingModeFromBackendID treats non-xAI backends as pay-as-you-go.
// xAI and unknown leave the decision to account-level auth_mode.
func GrokBillingModeFromBackendID(backendID string) BillingMode {
	if backendID == "" || backendID == grokDefaultBackendID {
		return BillingUnknown
	}
	return BillingPayAsYouGo
}

// GrokModelIDFromLines finds the most recent model id in an updates.jsonl tail.
func GrokModelIDFromLines(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		var parsed struct {
			Params *struct {
				Update *struct {
					ModelID string `json:"modelId"`
					Meta    *struct {
						ModelID string `json:"modelId"`
					} `json:"_meta"`
					Usage *struct {
						ModelUsage map[string]json.RawMessage `json:"modelUsage"`
					} `json:"usage"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed.Params == nil || parsed.Params.Update == nil {
			continue
		}
		u := parsed.Params.Update
		if u.ModelID != "" {
			return u.ModelID
		}
		if u.Meta != nil && u.Meta.ModelID != "" {
			return u.Meta.ModelID
		}
		if u.Usage != nil && len(u.Usage.ModelUsage) == 1 {
			for id := range u.Usage.ModelUsage {
				return id
			}
		}
	}
	return ""
}

// GrokModelIDFromSummaryJSON reads current_model_id from summary.json.
func GrokModelIDFromSummaryJSON(raw string) string {
	var parsed struct {
		CurrentModelID string `json:"current_model_id"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return parsed.CurrentModelID
}

// ResolveGrokBackendID is the label used for a pay-as-you-go Grok pane.
func ResolveGrokBackendID(modelID string, models map[string]GrokModelConfig, modelsBaseURL string) string {
	if id := GrokBackendIDForModel(modelID, models, modelsBaseURL); id != "" {
		return id
	}
	return grokDefaultBackendID
}
