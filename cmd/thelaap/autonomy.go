package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// The installed controller also runs from launchd when the window is closed.
// Only these fixed arguments cross the boundary. Model output cannot create a
// shell command; the controller validates choices against measured faults.
func autonomyArgs(action string) ([]string, bool) {
	switch action {
	case "status":
		return []string{"guardian", "status"}, true
	case "enable", "disable":
		return []string{"guardian", action}, true
	case "check":
		return []string{"guardian", "--repair"}, true
	}
	return nil, false
}

var autonomyJob struct {
	sync.Mutex
	running   bool
	errorText string
}

func callController(ctx context.Context, executable string, args []string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	isolateProcessGroup(cmd)
	cmd.Cancel = func() error { killGroup(cmd); return nil }
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("controllore: %s", trunc(withoutAnsi(string(output)), 600))
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("risposta del controllore non valida: %w", err)
	}
	return result, nil
}

func apiAutonomy(w http.ResponseWriter, r *http.Request) {
	executable := espandi(cfg().ControlloreStack)
	if executable == "" || !filepath.IsAbs(executable) {
		writeJSON(w, map[string]any{"available": false})
		return
	}
	action := "status"
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			errJSON(w, "richiesta non valida")
			return
		}
		action = req.Action
	} else if r.Method != http.MethodGet {
		errJSONStatus(w, http.StatusMethodNotAllowed, "usa GET o POST")
		return
	}
	args, allowed := autonomyArgs(action)
	if !allowed {
		errJSON(w, "azione non ammessa")
		return
	}
	if action == "check" {
		autonomyJob.Lock()
		if autonomyJob.running {
			autonomyJob.Unlock()
			errJSONStatus(w, http.StatusConflict, "controllo già in corso")
			return
		}
		autonomyJob.running, autonomyJob.errorText = true, ""
		autonomyJob.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			_, err := callController(ctx, executable, args)
			autonomyJob.Lock()
			defer autonomyJob.Unlock()
			autonomyJob.running = false
			if err != nil {
				autonomyJob.errorText = err.Error()
			}
		}()
		writeJSON(w, map[string]any{"ok": true, "running": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := callController(ctx, executable, args)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	result["available"] = true
	autonomyJob.Lock()
	result["running"], result["lastError"] = autonomyJob.running, autonomyJob.errorText
	autonomyJob.Unlock()
	writeJSON(w, result)
}
