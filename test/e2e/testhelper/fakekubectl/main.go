package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const stateDirEnv = "FAKE_KUBECTL_STATE_DIR"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	stateDir := strings.TrimSpace(os.Getenv(stateDirEnv))
	if stateDir == "" {
		return fmt.Errorf("%s must be set", stateDirEnv)
	}

	trimmed := stripGlobalArgs(args)
	if len(trimmed) == 0 {
		return fmt.Errorf("kubectl command cannot be empty")
	}

	switch trimmed[0] {
	case "get":
		return runGet(stateDir, trimmed[1:])
	case "apply":
		return runApply(stateDir, trimmed[1:])
	default:
		return fmt.Errorf("unsupported fake kubectl command %q", trimmed[0])
	}
}

func stripGlobalArgs(args []string) []string {
	trimmed := make([]string, 0, len(args))
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--kubeconfig" {
			skipNext = true
			continue
		}
		trimmed = append(trimmed, arg)
	}
	return trimmed
}

func runGet(stateDir string, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("kubectl get requires kind and name")
	}
	kind := args[0]
	name := args[1]
	namespace := namespaceArg(args[2:])
	path := objectStatePath(stateDir, kind, namespace, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Error from server (NotFound): %s %q not found", strings.ToLower(kind), name)
		}
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func runApply(stateDir string, args []string) error {
	manifestPath, err := filenameArg(args)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	applied := make([]string, 0)
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}
		var meta struct {
			Kind     string `json:"kind" yaml:"kind"`
			Metadata struct {
				Name      string `json:"name" yaml:"name"`
				Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
			} `json:"metadata" yaml:"metadata"`
		}
		if err := yaml.Unmarshal(raw.Raw, &meta); err != nil {
			return err
		}
		if meta.Kind == "" || meta.Metadata.Name == "" {
			return fmt.Errorf("applied document missing kind or metadata.name")
		}
		jsonData, err := yaml.YAMLToJSON(raw.Raw)
		if err != nil {
			return err
		}
		if !json.Valid(jsonData) {
			return fmt.Errorf("applied document for %s/%s did not marshal to valid JSON", meta.Kind, meta.Metadata.Name)
		}
		path := objectStatePath(stateDir, meta.Kind, meta.Metadata.Namespace, meta.Metadata.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, jsonData, 0o644); err != nil {
			return err
		}
		applied = append(applied, fmt.Sprintf("%s/%s configured", strings.ToLower(meta.Kind), meta.Metadata.Name))
	}
	if len(applied) == 0 {
		return fmt.Errorf("kubectl apply did not contain any objects")
	}
	_, err = os.Stdout.Write([]byte(strings.Join(applied, "\n")))
	return err
}

func namespaceArg(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" || args[i] == "--namespace" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
	}
	return ""
}

func filenameArg(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" || args[i] == "--filename" {
			if i+1 < len(args) {
				return args[i+1], nil
			}
			return "", fmt.Errorf("kubectl apply missing filename")
		}
	}
	return "", fmt.Errorf("kubectl apply requires -f")
}

func objectStatePath(stateDir, kind, namespace, name string) string {
	safeNamespace := namespace
	if strings.TrimSpace(safeNamespace) == "" {
		safeNamespace = "_cluster"
	}
	return filepath.Join(stateDir, strings.ToLower(kind), safeNamespace, name+".json")
}
