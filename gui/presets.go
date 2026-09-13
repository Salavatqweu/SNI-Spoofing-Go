package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sni-spoofing-go/guiapi"
	"sni-spoofing-go/helper/spawn"
)

type SniPreset = guiapi.SniPreset

var defaultSniPresets = []SniPreset{
	{FakeSNI: "developers.cloudflare.com", Upstream: "104.16.2.189"},
	{FakeSNI: "cdn.sstatic.net", Upstream: "198.252.206.17"},
	{FakeSNI: "cdnjs.cloudflare.com", Upstream: "104.17.24.14"},
}

func (a *App) presetsPath() (string, error) {
	exe, err := spawn.SelfExe()
	if err != nil { return "", err }
	return filepath.Join(filepath.Dir(exe), "presets.json"), nil
}

func validateSniPreset(p SniPreset) error {
	p.FakeSNI = strings.TrimSpace(p.FakeSNI)
	p.Upstream = strings.TrimSpace(p.Upstream)
	if p.FakeSNI == "" { return errors.New("fake SNI cannot be empty") }
	if strings.ContainsAny(p.FakeSNI, " \t\r\n/:\\") { return errors.New("fake SNI must be a hostname") }
	if net.ParseIP(p.Upstream) == nil { return errors.New("upstream must be a valid IP address") }
	return nil
}

func copyPresets(in []SniPreset) []SniPreset {
	out := append([]SniPreset(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].FakeSNI) < strings.ToLower(out[j].FakeSNI) })
	return out
}

func readSniPresets(path string) ([]SniPreset, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) { return copyPresets(defaultSniPresets), nil }
	if err != nil { return nil, err }
	var presets []SniPreset
	if err := json.Unmarshal(data, &presets); err != nil { return nil, fmt.Errorf("read presets: %w", err) }
	if presets == nil { presets = []SniPreset{} }
	for _, p := range presets { if err := validateSniPreset(p); err != nil { return nil, err } }
	return copyPresets(presets), nil
}

func writeSniPresets(path string, presets []SniPreset) error {
	data, err := json.MarshalIndent(copyPresets(presets), "", "  ")
	if err != nil { return err }
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil { return err }
	if err := os.Rename(tmp, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil { _ = os.Remove(tmp); return err }
		if retryErr := os.Rename(tmp, path); retryErr != nil { _ = os.Remove(tmp); return retryErr }
	}
	return nil
}

func (a *App) GetSniPresets() ([]SniPreset, error) {
	path, err := a.presetsPath()
	if err != nil { return nil, err }
	presets, err := readSniPresets(path)
	if err != nil { return nil, err }
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := writeSniPresets(path, presets); err != nil { return nil, err }
	}
	return presets, nil
}

func (a *App) SaveSniPreset(preset SniPreset) ([]SniPreset, error) {
	if err := validateSniPreset(preset); err != nil { return nil, err }
	path, err := a.presetsPath()
	if err != nil { return nil, err }
	presets, err := readSniPresets(path)
	if err != nil { return nil, err }
	key := strings.ToLower(strings.TrimSpace(preset.FakeSNI))
	found := false
	for i := range presets {
		if strings.ToLower(presets[i].FakeSNI) == key {
			presets[i] = SniPreset{FakeSNI: strings.TrimSpace(preset.FakeSNI), Upstream: strings.TrimSpace(preset.Upstream)}
			found = true
			break
		}
	}
	if !found { presets = append(presets, SniPreset{FakeSNI: strings.TrimSpace(preset.FakeSNI), Upstream: strings.TrimSpace(preset.Upstream)}) }
	if err := writeSniPresets(path, presets); err != nil { return nil, err }
	return copyPresets(presets), nil
}

func (a *App) DeleteSniPreset(fakeSNI string) ([]SniPreset, error) {
	path, err := a.presetsPath()
	if err != nil { return nil, err }
	presets, err := readSniPresets(path)
	if err != nil { return nil, err }
	key := strings.ToLower(strings.TrimSpace(fakeSNI))
	filtered := presets[:0]
	for _, p := range presets { if strings.ToLower(p.FakeSNI) != key { filtered = append(filtered, p) } }
	if err := writeSniPresets(path, filtered); err != nil { return nil, err }
	return copyPresets(filtered), nil
}
