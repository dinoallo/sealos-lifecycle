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
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"
)

const releaseLookupHTTPTimeout = 30 * time.Second

type ReleaseChannelLookupOptions struct {
	DistributionLine string
	Channel          ReleaseChannel
	Source           string
}

func ResolveReleaseChannelLookup(opts ReleaseChannelLookupOptions) (*ResolvedReleaseChannel, error) {
	line := strings.TrimSpace(opts.DistributionLine)
	if line == "" {
		return nil, fmt.Errorf("distribution line cannot be empty")
	}
	channel := opts.Channel
	if err := channel.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		return nil, fmt.Errorf("release source cannot be empty")
	}

	channelDoc, channelSubject, err := loadReleaseChannelFromSource(source, line, channel)
	if err != nil {
		return nil, err
	}
	if channelDoc.Distribution() != line {
		return nil, fmt.Errorf("release lookup %s/%s resolved distribution %q", line, channel, channelDoc.Distribution())
	}
	if channelDoc.Spec.Channel != channel {
		return nil, fmt.Errorf("release lookup %s/%s resolved channel %q", line, channel, channelDoc.Spec.Channel)
	}
	if strings.TrimSpace(channelDoc.Spec.BOMDigest) == "" {
		return nil, fmt.Errorf("release lookup %s/%s must resolve a digest-pinned BOM", line, channel)
	}

	bomData, bomSubject, err := loadBOMDataFromReleaseSource(source, channelSubject, strings.TrimSpace(channelDoc.Spec.BOMPath))
	if err != nil {
		return nil, err
	}
	bomDigest := digest.Canonical.FromBytes(bomData).String()
	if err := verifyReleaseChannelBOMDigest(channelDoc, bomDigest); err != nil {
		return nil, err
	}
	doc, err := LoadBytes(bomData, bomSubject)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseChannelBOM(channelDoc, doc); err != nil {
		return nil, err
	}
	doc.SetRuntimeChannel(channelDoc.Spec.Channel)
	return &ResolvedReleaseChannel{
		Channel:       channelDoc,
		BOM:           doc,
		BOMPath:       bomSubject,
		BOMDigest:     bomDigest,
		ChannelSource: channelSubject,
	}, nil
}

func loadReleaseChannelFromSource(source, line string, channel ReleaseChannel) (*ReleaseChannelDocument, string, error) {
	if isHTTPURL(source) {
		return loadReleaseChannelFromHTTP(source, line, channel)
	}
	return loadReleaseChannelFromLocalSource(source, line, channel)
}

func loadReleaseChannelFromLocalSource(source, line string, channel ReleaseChannel) (*ReleaseChannelDocument, string, error) {
	localSource := strings.TrimPrefix(source, "file://")
	info, err := os.Stat(localSource)
	if err != nil {
		return nil, "", fmt.Errorf("stat release source %q: %w", source, err)
	}
	if !info.IsDir() {
		doc, err := LoadReleaseChannelFile(localSource)
		return doc, localSource, err
	}
	for _, candidate := range localReleaseChannelCandidates(localSource, line, string(channel)) {
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("stat release channel candidate %q: %w", candidate, err)
		}
		doc, err := LoadReleaseChannelFile(candidate)
		return doc, candidate, err
	}
	return nil, "", fmt.Errorf("release channel %s/%s not found under source %q", line, channel, source)
}

func localReleaseChannelCandidates(root, line, channel string) []string {
	names := []string{channel + ".yaml", channel + ".yml", channel + ".json"}
	candidates := make([]string, 0, 12)
	for _, name := range names {
		candidates = append(candidates, filepath.Join(root, line, name))
		candidates = append(candidates, filepath.Join(root, "channels", line, name))
	}
	for _, ext := range []string{".yaml", ".yml", ".json"} {
		candidates = append(candidates, filepath.Join(root, line+"-"+channel+ext))
	}
	return candidates
}

func loadReleaseChannelFromHTTP(source, line string, channel ReleaseChannel) (*ReleaseChannelDocument, string, error) {
	endpoint, err := releaseChannelLookupURL(source, line, string(channel))
	if err != nil {
		return nil, "", err
	}
	data, err := readHTTPDocument(endpoint)
	if err != nil {
		return nil, "", err
	}
	var doc ReleaseChannelDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("unmarshal release channel %q: %w", endpoint, err)
	}
	doc.Normalize()
	if err := doc.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate release channel %q: %w", endpoint, err)
	}
	return &doc, endpoint, nil
}

func releaseChannelLookupURL(source, line, channel string) (string, error) {
	base, err := url.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse release source %q: %w", source, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("release source %q must use http or https", source)
	}
	base.Path = joinURLPath(base.Path, "v1", "distributions", line, "channels", channel)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func loadBOMDataFromReleaseSource(source, channelSubject, bomPath string) ([]byte, string, error) {
	if bomPath == "" {
		return nil, "", fmt.Errorf("spec.bomPath cannot be empty")
	}
	if isHTTPURL(bomPath) {
		data, err := readHTTPDocument(bomPath)
		return data, bomPath, err
	}
	if isHTTPURL(channelSubject) {
		location, err := resolveRelativeURL(channelSubject, bomPath)
		if err != nil {
			return nil, "", err
		}
		data, err := readHTTPDocument(location)
		return data, location, err
	}
	localPath := strings.TrimPrefix(bomPath, "file://")
	if filepath.IsAbs(localPath) {
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, "", fmt.Errorf("read bom %q: %w", localPath, err)
		}
		return data, localPath, nil
	}
	if strings.TrimSpace(channelSubject) != "" && !isHTTPURL(channelSubject) {
		localPath = filepath.Join(filepath.Dir(strings.TrimPrefix(channelSubject, "file://")), localPath)
	} else {
		localSource := strings.TrimPrefix(source, "file://")
		localPath = filepath.Join(localSource, localPath)
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, "", fmt.Errorf("read bom %q: %w", localPath, err)
	}
	return data, localPath, nil
}

func resolveRelativeURL(baseURL, relative string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse release channel URL %q: %w", baseURL, err)
	}
	ref, err := url.Parse(relative)
	if err != nil {
		return "", fmt.Errorf("parse bom path %q: %w", relative, err)
	}
	return base.ResolveReference(ref).String(), nil
}

func readHTTPDocument(location string) ([]byte, error) {
	client := &http.Client{Timeout: releaseLookupHTTPTimeout}
	resp, err := client.Get(location)
	if err != nil {
		return nil, fmt.Errorf("GET %q: %w", location, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GET %q: unexpected status %s", location, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %q response: %w", location, err)
	}
	return data, nil
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func joinURLPath(elements ...string) string {
	cleaned := make([]string, 0, len(elements))
	for _, element := range elements {
		element = strings.Trim(element, "/")
		if element == "" {
			continue
		}
		cleaned = append(cleaned, element)
	}
	if len(cleaned) == 0 {
		return "/"
	}
	return "/" + path.Join(cleaned...)
}
