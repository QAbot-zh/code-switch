package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

const (
	pricingOverrideFile = "model_prices.json"
	pricingMetaFile     = "model_prices.meta.json"
	pricingUpstreamURL  = "https://raw.githubusercontent.com/BerriAI/litellm/litellm_internal_staging/model_prices_and_context_window.json"
	pricingMinEntries   = 50
	pricingHTTPTimeout  = 30 * time.Second
)

// PricingStatus 描述当前定价数据来源，用于前端展示。
type PricingStatus struct {
	Source       string `json:"source"`
	UpdatedAt    string `json:"updated_at"`
	ModelCount   int    `json:"model_count"`
	FetchedURL   string `json:"fetched_url,omitempty"`
	ImportedPath string `json:"imported_path,omitempty"`
	UpstreamURL  string `json:"upstream_url"`
	OverridePath string `json:"override_path"`
}

// PricingService 管理用户级覆盖文件与拉取/导入逻辑。
type PricingService struct {
	mu           sync.Mutex
	pricing      *modelpricing.Service
	overridePath string
	metaPath     string
	httpClient   *http.Client
}

// NewPricingService 创建实例，覆盖文件存放在 ~/.codex-switch/。
func NewPricingService() *PricingService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, appSettingsDir)
	svc, err := modelpricing.DefaultService()
	if err != nil {
		log.Printf("pricing service init failed: %v", err)
	}
	return &PricingService{
		pricing:      svc,
		overridePath: filepath.Join(dir, pricingOverrideFile),
		metaPath:     filepath.Join(dir, pricingMetaFile),
		httpClient:   &http.Client{Timeout: pricingHTTPTimeout},
	}
}

// Start 启动时尝试加载已存在的覆盖文件；解析失败保留内置数据。
func (ps *PricingService) Start() error {
	if ps == nil || ps.pricing == nil {
		return nil
	}
	data, err := os.ReadFile(ps.overridePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.Printf("pricing override read failed: %v", err)
		return nil
	}
	count, err := validatePricingPayload(data)
	if err != nil {
		log.Printf("pricing override invalid, keep builtin: %v", err)
		return nil
	}
	meta := ps.loadMeta()
	meta.ModelCount = count
	if meta.Source == "" || meta.Source == "builtin" {
		meta.Source = "custom"
	}
	if meta.UpdatedAt == "" {
		if info, statErr := os.Stat(ps.overridePath); statErr == nil {
			meta.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	if reloadErr := ps.pricing.ReloadFromBytes(data, meta); reloadErr != nil {
		log.Printf("pricing override reload failed: %v", reloadErr)
	}
	return nil
}

func (ps *PricingService) Stop() error { return nil }

// GetStatus 返回当前 pricing 状态。
func (ps *PricingService) GetStatus() (PricingStatus, error) {
	return ps.statusFromService(), nil
}

// UpdateFromUpstream 从内置 URL 拉取最新 JSON 并应用。
func (ps *PricingService) UpdateFromUpstream() (PricingStatus, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.pricing == nil {
		return PricingStatus{}, fmt.Errorf("pricing service not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), pricingHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pricingUpstreamURL, nil)
	if err != nil {
		return ps.statusFromService(), fmt.Errorf("build request: %w", err)
	}
	resp, err := ps.httpClient.Do(req)
	if err != nil {
		return ps.statusFromService(), fmt.Errorf("fetch upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ps.statusFromService(), fmt.Errorf("upstream returned %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ps.statusFromService(), fmt.Errorf("read upstream body: %w", err)
	}
	count, err := validatePricingPayload(data)
	if err != nil {
		return ps.statusFromService(), err
	}
	meta := modelpricing.SourceMeta{
		Source:     "upstream",
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		FetchedURL: pricingUpstreamURL,
		ModelCount: count,
	}
	if err := ps.persistOverride(data, meta); err != nil {
		return ps.statusFromService(), err
	}
	if err := ps.pricing.ReloadFromBytes(data, meta); err != nil {
		return ps.statusFromService(), err
	}
	return ps.statusFromService(), nil
}

// UpdateFromFile 用本地 JSON 文件覆盖。
func (ps *PricingService) UpdateFromFile(path string) (PricingStatus, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.pricing == nil {
		return PricingStatus{}, fmt.Errorf("pricing service not initialized")
	}
	clean := strings.TrimSpace(path)
	if clean == "" {
		return ps.statusFromService(), fmt.Errorf("file path is required")
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return ps.statusFromService(), fmt.Errorf("read file: %w", err)
	}
	count, err := validatePricingPayload(data)
	if err != nil {
		return ps.statusFromService(), err
	}
	meta := modelpricing.SourceMeta{
		Source:       "custom",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		ImportedPath: clean,
		ModelCount:   count,
	}
	if err := ps.persistOverride(data, meta); err != nil {
		return ps.statusFromService(), err
	}
	if err := ps.pricing.ReloadFromBytes(data, meta); err != nil {
		return ps.statusFromService(), err
	}
	return ps.statusFromService(), nil
}

// ResetToBuiltin 删除覆盖文件并重新加载内置数据。
func (ps *PricingService) ResetToBuiltin() (PricingStatus, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.pricing == nil {
		return PricingStatus{}, fmt.Errorf("pricing service not initialized")
	}
	if err := os.Remove(ps.overridePath); err != nil && !os.IsNotExist(err) {
		return ps.statusFromService(), fmt.Errorf("remove override: %w", err)
	}
	if err := os.Remove(ps.metaPath); err != nil && !os.IsNotExist(err) {
		return ps.statusFromService(), fmt.Errorf("remove meta: %w", err)
	}
	if err := ps.pricing.ResetToBuiltin(); err != nil {
		return ps.statusFromService(), err
	}
	return ps.statusFromService(), nil
}

func (ps *PricingService) statusFromService() PricingStatus {
	status := PricingStatus{
		UpstreamURL:  pricingUpstreamURL,
		OverridePath: ps.overridePath,
	}
	if ps.pricing == nil {
		status.Source = "builtin"
		return status
	}
	meta := ps.pricing.Snapshot()
	status.Source = meta.Source
	if status.Source == "" {
		status.Source = "builtin"
	}
	status.UpdatedAt = meta.UpdatedAt
	status.ModelCount = meta.ModelCount
	status.FetchedURL = meta.FetchedURL
	status.ImportedPath = meta.ImportedPath
	return status
}

func (ps *PricingService) persistOverride(data []byte, meta modelpricing.SourceMeta) error {
	if err := os.MkdirAll(filepath.Dir(ps.overridePath), 0o755); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}
	if err := writeAtomic(ps.overridePath, data, 0o644); err != nil {
		return fmt.Errorf("write override: %w", err)
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := writeAtomic(ps.metaPath, metaBytes, 0o644); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}
	return nil
}

func (ps *PricingService) loadMeta() modelpricing.SourceMeta {
	data, err := os.ReadFile(ps.metaPath)
	if err != nil {
		return modelpricing.SourceMeta{}
	}
	var meta modelpricing.SourceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return modelpricing.SourceMeta{}
	}
	return meta
}

func validatePricingPayload(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty payload")
	}
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(raw) < pricingMinEntries {
		return 0, fmt.Errorf("only %d entries, expected at least %d", len(raw), pricingMinEntries)
	}
	return len(raw), nil
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
