/**
 * Wrapper around the herdr CLI (agent-agnostic).
 */
package herdrcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

const spawnTimeout = 3 * time.Second

// Source is the report-metadata source id for this plugin.
const Source = "usagebar"

func herdrBin() string {
	if v := os.Getenv("HERDR_BIN_PATH"); v != "" {
		return v
	}
	return "herdr"
}

func spawnHerdr(args ...string) (stdout string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), spawnTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, herdrBin(), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return out.String(), true
}

// PaneInfo is agent / status / session / cwd for a pane.
type PaneInfo struct {
	Agent         *string
	AgentStatus   *string
	AgentSession  *provider.AgentSession
	RowLabel      *string
	Cwd           *string
	ForegroundCwd *string
}

// RawPaneListEntry is a pane list row from herdr (for pure buildOpenAgentPanes).
type RawPaneListEntry struct {
	PaneID        string
	Agent         string
	Label         string
	DisplayAgent  string
	Cwd           string
	ForegroundCwd string
	WorkspaceID   string
	TabID         string
	SessionKind   string
	SessionValue  string
}

// OpenAgentPane is an open agent pane with id.
type OpenAgentPane struct {
	PaneID string
	PaneInfo
}

// PaneSessionCwd chooses the project directory used to resolve local session
// files. OMP/Pi can leave a language server in the foreground with a cwd
// inside a virtualenv; without an explicit agent session their pane cwd is
// the stable project identity. Other harnesses retain foreground-cwd priority.
func PaneSessionCwd(pane PaneInfo) *string {
	if pane.Agent != nil && (*pane.Agent == "omp" || *pane.Agent == "pi") && pane.AgentSession == nil && pane.Cwd != nil {
		return pane.Cwd
	}
	if pane.ForegroundCwd != nil {
		return pane.ForegroundCwd
	}
	return pane.Cwd
}

func firstNonEmpty(values ...string) *string {
	for _, v := range values {
		if v != "" {
			return &v
		}
	}
	return nil
}

func parseAgentSession(kind, value string) *provider.AgentSession {
	if kind == "" || value == "" {
		return nil
	}
	return &provider.AgentSession{Kind: kind, Value: value}
}

// ResolveRowLabel priority: pane rename > tab label > display_agent > agent.
func ResolveRowLabel(label, displayAgent, agent, tabLabel string) *string {
	return firstNonEmpty(label, tabLabel, displayAgent, agent)
}

// BuildOpenAgentPanes is the pure core of listOpenAgentPanes.
func BuildOpenAgentPanes(panes []RawPaneListEntry, tabLabelsByTabID map[string]string) []OpenAgentPane {
	var out []OpenAgentPane
	for _, pane := range panes {
		if pane.PaneID == "" || pane.Agent == "" {
			continue
		}
		tabLabel := ""
		if pane.TabID != "" {
			tabLabel = tabLabelsByTabID[pane.TabID]
		}
		row := ResolveRowLabel(pane.Label, pane.DisplayAgent, pane.Agent, tabLabel)
		agent := pane.Agent
		info := PaneInfo{
			Agent:        &agent,
			AgentSession: parseAgentSession(pane.SessionKind, pane.SessionValue),
			RowLabel:     row,
		}
		if pane.Cwd != "" {
			c := pane.Cwd
			info.Cwd = &c
		}
		if pane.ForegroundCwd != "" {
			c := pane.ForegroundCwd
			info.ForegroundCwd = &c
		}
		out = append(out, OpenAgentPane{PaneID: pane.PaneID, PaneInfo: info})
	}
	return out
}

// GetPaneInfo fetches pane get JSON for paneId.
func GetPaneInfo(paneID string) PaneInfo {
	stdout, ok := spawnHerdr("pane", "get", paneID)
	if !ok || stdout == "" {
		return PaneInfo{}
	}
	var parsed struct {
		Result *struct {
			Pane *struct {
				Agent         *string `json:"agent"`
				AgentStatus   *string `json:"agent_status"`
				Label         *string `json:"label"`
				DisplayAgent  *string `json:"display_agent"`
				Cwd           *string `json:"cwd"`
				ForegroundCwd *string `json:"foreground_cwd"`
				AgentSession  *struct {
					Kind  *string `json:"kind"`
					Value *string `json:"value"`
				} `json:"agent_session"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil || parsed.Result == nil || parsed.Result.Pane == nil {
		return PaneInfo{}
	}
	p := parsed.Result.Pane
	var session *provider.AgentSession
	if p.AgentSession != nil && p.AgentSession.Kind != nil && p.AgentSession.Value != nil {
		session = parseAgentSession(*p.AgentSession.Kind, *p.AgentSession.Value)
	}
	agent := p.Agent
	rowLabel := firstNonEmpty(
		deref(p.Label),
		deref(p.DisplayAgent),
		deref(agent),
	)
	return PaneInfo{
		Agent:         agent,
		AgentStatus:   p.AgentStatus,
		AgentSession:  session,
		RowLabel:      rowLabel,
		Cwd:           p.Cwd,
		ForegroundCwd: p.ForegroundCwd,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetSidebarWidthColumns returns layout area.x for the pane (sidebar width).
func GetSidebarWidthColumns(paneID string) *int {
	stdout, ok := spawnHerdr("pane", "layout", "--pane", paneID)
	if !ok || stdout == "" {
		return nil
	}
	var parsed struct {
		Result *struct {
			Layout *struct {
				Area *struct {
					X *float64 `json:"x"`
				} `json:"area"`
			} `json:"layout"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return nil
	}
	if parsed.Result == nil || parsed.Result.Layout == nil || parsed.Result.Layout.Area == nil || parsed.Result.Layout.Area.X == nil {
		return nil
	}
	x := *parsed.Result.Layout.Area.X
	if x <= 0 {
		return nil
	}
	v := int(x)
	return &v
}

// SetMetadataToken reports one named presentation token for use in configurable
// Herdr 0.7.4+ sidebar rows (for example $limit).
func SetMetadataToken(paneID, source, name, value string) bool {
	_, ok := spawnHerdr("pane", "report-metadata", paneID, "--source", source, "--token", name+"="+value)
	return ok
}

// ClearMetadataToken removes one named presentation token owned by source.
func ClearMetadataToken(paneID, source, name string) bool {
	_, ok := spawnHerdr("pane", "report-metadata", paneID, "--source", source, "--clear-token", name)
	return ok
}

// ShowNotification runs herdr notification show; returns whether shown.
func ShowNotification(title, body string) bool {
	stdout, ok := spawnHerdr("notification", "show", title, "--body", body)
	if !ok || stdout == "" {
		return false
	}
	var parsed struct {
		Result *struct {
			Shown *bool `json:"shown"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return false
	}
	return parsed.Result != nil && parsed.Result.Shown != nil && *parsed.Result.Shown
}

// ListOpenAgentPanes lists panes that have an attached agent.
func ListOpenAgentPanes() []OpenAgentPane {
	panes, _ := ListOpenAgentPanesOK()
	return panes
}

// ListOpenAgentPanesOK is ListOpenAgentPanes plus whether the pane query
// succeeded, so callers can distinguish "no agent panes open" (ok=true,
// empty) from "herdr query failed" (ok=false).
func ListOpenAgentPanesOK() ([]OpenAgentPane, bool) {
	stdout, ok := spawnHerdr("pane", "list")
	if !ok || stdout == "" {
		return nil, false
	}
	var parsed struct {
		Result *struct {
			Panes []struct {
				PaneID        *string `json:"pane_id"`
				Agent         *string `json:"agent"`
				Label         *string `json:"label"`
				DisplayAgent  *string `json:"display_agent"`
				Cwd           *string `json:"cwd"`
				ForegroundCwd *string `json:"foreground_cwd"`
				WorkspaceID   *string `json:"workspace_id"`
				TabID         *string `json:"tab_id"`
				AgentSession  *struct {
					Kind  *string `json:"kind"`
					Value *string `json:"value"`
				} `json:"agent_session"`
			} `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil || parsed.Result == nil {
		return nil, false
	}
	workspaceIDs := map[string]struct{}{}
	var raw []RawPaneListEntry
	for _, p := range parsed.Result.Panes {
		entry := RawPaneListEntry{
			PaneID: deref(p.PaneID),
			Agent:  deref(p.Agent),
		}
		entry.Label = deref(p.Label)
		entry.DisplayAgent = deref(p.DisplayAgent)
		entry.Cwd = deref(p.Cwd)
		entry.ForegroundCwd = deref(p.ForegroundCwd)
		entry.WorkspaceID = deref(p.WorkspaceID)
		entry.TabID = deref(p.TabID)
		if p.AgentSession != nil {
			entry.SessionKind = deref(p.AgentSession.Kind)
			entry.SessionValue = deref(p.AgentSession.Value)
		}
		if entry.WorkspaceID != "" {
			workspaceIDs[entry.WorkspaceID] = struct{}{}
		}
		raw = append(raw, entry)
	}
	tabLabels := map[string]string{}
	for ws := range workspaceIDs {
		for k, v := range fetchTabLabels(ws) {
			tabLabels[k] = v
		}
	}
	return BuildOpenAgentPanes(raw, tabLabels), true
}

func fetchTabLabels(workspaceID string) map[string]string {
	stdout, ok := spawnHerdr("tab", "list", "--workspace", workspaceID)
	if !ok || stdout == "" {
		return nil
	}
	var parsed struct {
		Result *struct {
			Tabs []struct {
				TabID *string `json:"tab_id"`
				Label *string `json:"label"`
			} `json:"tabs"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil || parsed.Result == nil {
		return nil
	}
	out := map[string]string{}
	for _, tab := range parsed.Result.Tabs {
		if tab.TabID != nil && *tab.TabID != "" && tab.Label != nil && *tab.Label != "" {
			out[*tab.TabID] = *tab.Label
		}
	}
	return out
}
