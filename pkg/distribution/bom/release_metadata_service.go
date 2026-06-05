// Copyright 2026 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bom

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	promotionpolicy "github.com/labring/sealos/pkg/distribution/promotion"
)

type ReleaseMetadataService struct {
	Source string
}

type ReleasePromotionRequest struct {
	TargetRevision   string                   `json:"targetRevision" yaml:"targetRevision"`
	SourceChannel    ReleaseChannel           `json:"sourceChannel,omitempty" yaml:"sourceChannel,omitempty"`
	HealthProof      *DistributionHealthProof `json:"healthProof,omitempty" yaml:"healthProof,omitempty"`
	HealthProofPath  string                   `json:"healthProofPath,omitempty" yaml:"healthProofPath,omitempty"`
	ValidationCohort string                   `json:"validationCohort,omitempty" yaml:"validationCohort,omitempty"`
	Reason           string                   `json:"reason" yaml:"reason"`
	ApprovedBy       string                   `json:"approvedBy" yaml:"approvedBy"`
	ApprovedAt       string                   `json:"approvedAt,omitempty" yaml:"approvedAt,omitempty"`
}

type ReleasePromotionResponse struct {
	Line                 string                    `json:"line" yaml:"line"`
	Channel              string                    `json:"channel" yaml:"channel"`
	ReleaseChannel       string                    `json:"releaseChannelPath" yaml:"releaseChannelPath"`
	BOMPath              string                    `json:"bomPath" yaml:"bomPath"`
	FromRevision         string                    `json:"fromRevision" yaml:"fromRevision"`
	ToRevision           string                    `json:"toRevision" yaml:"toRevision"`
	Changed              bool                      `json:"changed" yaml:"changed"`
	Promotion            DistributionPromotionRef  `json:"promotion" yaml:"promotion"`
	PolicyDecision       *promotionpolicy.Decision `json:"policyDecision,omitempty" yaml:"policyDecision,omitempty"`
	HealthProofPath      string                    `json:"healthProofPath,omitempty" yaml:"healthProofPath,omitempty"`
	CandidatePath        string                    `json:"candidatePath,omitempty" yaml:"candidatePath,omitempty"`
	PromotionHistoryPath string                    `json:"promotionHistoryPath,omitempty" yaml:"promotionHistoryPath,omitempty"`
}

func NewReleaseMetadataHandler(source string) (http.Handler, error) {
	service := &ReleaseMetadataService{
		Source: strings.TrimPrefix(strings.TrimSpace(source), "file://"),
	}
	if service.Source == "" {
		return nil, fmt.Errorf("release source cannot be empty")
	}
	info, err := os.Stat(service.Source)
	if err != nil {
		return nil, fmt.Errorf("stat release source %q: %w", source, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("release source %q must be a directory", source)
	}
	return service, nil
}

func (s *ReleaseMetadataService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := splitReleaseMetadataPath(r.URL.Path)
	if len(parts) == 6 && parts[0] == "v1" && parts[1] == "distributions" && parts[3] == "channels" && parts[5] == "promotions" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.servePromotion(w, r, parts[2], parts[4])
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch {
	case len(parts) == 5 && parts[0] == "v1" && parts[1] == "distributions" && parts[3] == "channels":
		s.serveChannel(w, r, parts[2], parts[4])
	case len(parts) == 6 && parts[0] == "v1" && parts[1] == "distributions" && parts[3] == "revisions" && parts[5] == "bom":
		s.serveBOM(w, r, parts[2], parts[4])
	default:
		http.NotFound(w, r)
	}
}

func (s *ReleaseMetadataService) serveChannel(w http.ResponseWriter, r *http.Request, line, channelValue string) {
	channel := ReleaseChannel(channelValue)
	resolved, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: line,
		Channel:          channel,
		Source:           s.Source,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	doc := *resolved.Channel
	doc.Spec.BOMPath = releaseMetadataBOMEndpoint(line, doc.Spec.TargetRevision)
	if strings.TrimSpace(doc.Spec.BOMDigest) == "" {
		doc.Spec.BOMDigest = resolved.BOMDigest
	}
	writeReleaseMetadataYAML(w, r, &doc)
}

func (s *ReleaseMetadataService) serveBOM(w http.ResponseWriter, r *http.Request, line, revision string) {
	data, err := loadReleaseMetadataBOMData(s.Source, line, revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeReleaseMetadataBytes(w, r, data)
}

func (s *ReleaseMetadataService) servePromotion(w http.ResponseWriter, r *http.Request, line, channelValue string) {
	request, err := decodeReleasePromotionRequest(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, healthProofPath, err := s.promote(line, ReleaseChannel(channelValue), request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeReleaseMetadataYAML(w, r, ReleasePromotionResponse{
		Line:                 result.Channel.Distribution(),
		Channel:              string(result.Channel.Spec.Channel),
		ReleaseChannel:       result.ChannelPath,
		BOMPath:              result.BOMPath,
		FromRevision:         result.FromRevision,
		ToRevision:           result.ToRevision,
		Changed:              result.Changed,
		Promotion:            result.Promotion,
		PolicyDecision:       result.Decision,
		HealthProofPath:      healthProofPath,
		CandidatePath:        result.CandidatePath,
		PromotionHistoryPath: result.PromotionHistoryPath,
	})
}

func (s *ReleaseMetadataService) promote(line string, channel ReleaseChannel, request ReleasePromotionRequest) (*PromoteReleaseChannelResult, string, error) {
	targetRevision := strings.TrimSpace(request.TargetRevision)
	if targetRevision == "" {
		return nil, "", fmt.Errorf("targetRevision cannot be empty")
	}
	channelPath, channelDoc, err := resolveReleaseMetadataChannelPath(s.Source, line, channel)
	if err != nil {
		return nil, "", err
	}
	if channelDoc.Distribution() != line {
		return nil, "", fmt.Errorf("release channel %q distribution %q does not match requested line %q", channelDoc.Metadata.Name, channelDoc.Distribution(), line)
	}
	targetBOMPath, err := resolveReleaseMetadataBOMPath(s.Source, line, targetRevision)
	if err != nil {
		return nil, "", err
	}
	healthProofPath := strings.TrimSpace(request.HealthProofPath)
	if request.HealthProof != nil {
		healthProofPath, err = writeReleaseMetadataHealthProof(s.Source, line, targetRevision, request.HealthProof)
		if err != nil {
			return nil, "", err
		}
	}
	var approvedAt time.Time
	if strings.TrimSpace(request.ApprovedAt) != "" {
		approvedAt, err = time.Parse(time.RFC3339, strings.TrimSpace(request.ApprovedAt))
		if err != nil {
			return nil, "", fmt.Errorf("approvedAt must be RFC3339: %w", err)
		}
	}
	result, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:      channelPath,
		TargetBOMPath:    targetBOMPath,
		ReleaseStoreRoot: s.Source,
		SourceChannel:    request.SourceChannel,
		ValidationCohort: request.ValidationCohort,
		HealthProofPath:  healthProofPath,
		Reason:           request.Reason,
		ApprovedBy:       request.ApprovedBy,
		ApprovedAt:       approvedAt,
	})
	if err != nil {
		return nil, "", err
	}
	return result, healthProofPath, nil
}

func decodeReleasePromotionRequest(w http.ResponseWriter, r *http.Request) (ReleasePromotionRequest, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return ReleasePromotionRequest{}, fmt.Errorf("read promotion request: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return ReleasePromotionRequest{}, fmt.Errorf("promotion request body cannot be empty")
	}
	var request ReleasePromotionRequest
	if err := yaml.Unmarshal(data, &request); err != nil {
		return ReleasePromotionRequest{}, fmt.Errorf("unmarshal promotion request: %w", err)
	}
	return request, nil
}

func splitReleaseMetadataPath(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return nil
	}
	rawParts := strings.Split(trimmed, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part == "" {
			continue
		}
		unescaped, err := url.PathUnescape(part)
		if err != nil {
			parts = append(parts, part)
			continue
		}
		parts = append(parts, unescaped)
	}
	return parts
}

func resolveReleaseMetadataChannelPath(source, line string, channel ReleaseChannel) (string, *ReleaseChannelDocument, error) {
	if err := channel.ValidateRequired(); err != nil {
		return "", nil, fmt.Errorf("channel: %w", err)
	}
	for _, candidate := range localReleaseChannelCandidates(source, line, string(channel)) {
		doc, err := LoadReleaseChannelFile(candidate)
		if err != nil {
			if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
				continue
			}
			return "", nil, err
		}
		return candidate, doc, nil
	}
	return "", nil, fmt.Errorf("release channel %s/%s not found under source %q", line, channel, source)
}

func releaseMetadataBOMEndpoint(line, revision string) string {
	return "/v1/distributions/" + url.PathEscape(strings.TrimSpace(line)) + "/revisions/" + url.PathEscape(strings.TrimSpace(revision)) + "/bom"
}

func loadReleaseMetadataBOMData(source, line, revision string) ([]byte, error) {
	path, err := resolveReleaseMetadataBOMPath(source, line, revision)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release BOM %q: %w", path, err)
	}
	return data, nil
}

func resolveReleaseMetadataBOMPath(source, line, revision string) (string, error) {
	for _, candidate := range releaseMetadataBOMCandidates(source, line, revision) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read release BOM %q: %w", candidate, err)
		}
		doc, err := LoadBytes(data, candidate)
		if err != nil {
			return "", err
		}
		if doc.Metadata.Name != line {
			return "", fmt.Errorf("release BOM %q metadata.name %q does not match line %q", candidate, doc.Metadata.Name, line)
		}
		if doc.Spec.Revision != revision {
			return "", fmt.Errorf("release BOM %q revision %q does not match requested revision %q", candidate, doc.Spec.Revision, revision)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("release BOM %s/%s not found under source %q", line, revision, source)
}

func releaseMetadataBOMCandidates(root, line, revision string) []string {
	names := []string{"bom.yaml", "bom.yml", "bom.json"}
	candidates := make([]string, 0, 12)
	for _, name := range names {
		candidates = append(candidates, filepath.Join(root, "releases", line, revision, name))
		candidates = append(candidates, filepath.Join(root, line, revision, name))
	}
	for _, ext := range []string{".yaml", ".yml", ".json"} {
		candidates = append(candidates, filepath.Join(root, "releases", line, revision+ext))
		candidates = append(candidates, filepath.Join(root, "boms", revision+ext))
	}
	return candidates
}

func writeReleaseMetadataHealthProof(source, line, revision string, proof *DistributionHealthProof) (string, error) {
	if proof == nil {
		return "", nil
	}
	if err := proof.Validate(); err != nil {
		return "", fmt.Errorf("validate healthProof: %w", err)
	}
	if proof.Spec.Line != line {
		return "", fmt.Errorf("healthProof line %q does not match requested line %q", proof.Spec.Line, line)
	}
	if proof.Spec.TargetRevision != revision {
		return "", fmt.Errorf("healthProof targetRevision %q does not match requested revision %q", proof.Spec.TargetRevision, revision)
	}
	name := releaseMetadataSafeName(proof.Metadata.Name)
	if name == "" {
		name = "health-proof"
	}
	path := filepath.Join(source, "proofs", line, revision, name+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create health proof directory: %w", err)
	}
	data, err := yaml.Marshal(proof)
	if err != nil {
		return "", fmt.Errorf("marshal healthProof: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write health proof %q: %w", path, err)
	}
	return path, nil
}

func releaseMetadataSafeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeReleaseMetadataYAML(w http.ResponseWriter, r *http.Request, value any) {
	data, err := yaml.Marshal(value)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal response: %v", err), http.StatusInternalServerError)
		return
	}
	writeReleaseMetadataBytes(w, r, data)
}

func writeReleaseMetadataBytes(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
