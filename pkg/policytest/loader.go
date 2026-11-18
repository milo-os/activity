package policytest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/kustomize/api/krusty"
	kusttypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	sigsyaml "sigs.k8s.io/yaml"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const activityPolicyAPIVersion = "activity.miloapis.com/v1alpha1"

var kustomizeFileNames = []string{
	"kustomization.yaml",
	"kustomization.yml",
	"Kustomization",
}

// IsTestFile reports whether filename uses a recognized policy test suffix.
func IsTestFile(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	for _, suffix := range []string{
		".test.yaml",
		".tests.yaml",
		"-test.yaml",
		"-tests.yaml",
		".test.yml",
		".tests.yml",
		"-test.yml",
		"-tests.yml",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// DiscoverTestFiles resolves test files from the provided file or directory paths.
func DiscoverTestFiles(paths []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string

	for _, p := range paths {
		cleanPath := filepath.Clean(p)
		info, err := os.Stat(cleanPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}

		if !info.IsDir() {
			abs, err := filepath.Abs(cleanPath)
			if err != nil {
				abs = cleanPath
			}
			if _, ok := seen[abs]; !ok {
				seen[abs] = struct{}{}
				files = append(files, cleanPath)
			}
			continue
		}

		var dirFiles []string
		err = filepath.WalkDir(cleanPath, func(entryPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if IsTestFile(entryPath) {
				dirFiles = append(dirFiles, entryPath)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", p, err)
		}
		sort.Strings(dirFiles)
		for _, f := range dirFiles {
			abs, err := filepath.Abs(f)
			if err != nil {
				abs = f
			}
			if _, ok := seen[abs]; !ok {
				seen[abs] = struct{}{}
				files = append(files, f)
			}
		}
	}

	return files, nil
}

// LoadPoliciesFromPaths loads ActivityPolicy resources from files, directories,
// or Kustomize directories/components. Non-ActivityPolicy manifests inside a
// kustomization are ignored.
func LoadPoliciesFromPaths(paths []string) ([]LoadedPolicy, []ValidationIssue) {
	var policies []LoadedPolicy
	var issues []ValidationIssue

	for _, p := range paths {
		loaded, loadIssues := loadPoliciesFromPath(filepath.Clean(p))
		policies = append(policies, loaded...)
		issues = append(issues, loadIssues...)
	}

	return policies, issues
}

func loadPoliciesFromPath(path string) ([]LoadedPolicy, []ValidationIssue) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   path,
			Message:  fmt.Sprintf("failed to access path: %v", err),
			Severity: "error",
		}}
	}

	if !info.IsDir() {
		return loadPoliciesFromFile(path)
	}

	if hasKustomizationFile(path) {
		return loadPoliciesFromKustomization(path)
	}

	var policies []LoadedPolicy
	var issues []ValidationIssue

	err = filepath.WalkDir(path, func(entryPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			issues = append(issues, ValidationIssue{
				Source:   entryPath,
				Message:  fmt.Sprintf("failed to read directory entry: %v", walkErr),
				Severity: "error",
			})
			return nil
		}

		if d.IsDir() {
			if entryPath != path && hasKustomizationFile(entryPath) {
				loaded, loadIssues := loadPoliciesFromKustomization(entryPath)
				policies = append(policies, loaded...)
				issues = append(issues, loadIssues...)
				return filepath.SkipDir
			}
			return nil
		}

		if isKustomizationFilename(filepath.Base(entryPath)) || IsTestFile(entryPath) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entryPath))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		loaded, loadIssues := loadPoliciesFromFile(entryPath)
		policies = append(policies, loaded...)
		issues = append(issues, loadIssues...)
		return nil
	})
	if err != nil {
		issues = append(issues, ValidationIssue{
			Source:   path,
			Message:  fmt.Sprintf("failed to walk directory: %v", err),
			Severity: "error",
		})
	}

	return policies, issues
}

func hasKustomizationFile(dir string) bool {
	for _, name := range kustomizeFileNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func isKustomizationFilename(name string) bool {
	for _, candidate := range kustomizeFileNames {
		if name == candidate {
			return true
		}
	}
	return false
}

func loadPoliciesFromKustomization(dir string) ([]LoadedPolicy, []ValidationIssue) {
	opts := &krusty.Options{
		LoadRestrictions: kusttypes.LoadRestrictionsNone,
		PluginConfig:     kusttypes.DisabledPluginConfig(),
	}
	k := krusty.MakeKustomizer(opts)
	resMap, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   dir,
			Message:  fmt.Sprintf("failed to build kustomization: %v", err),
			Severity: "error",
		}}
	}

	yamlBytes, err := resMap.AsYaml()
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   dir,
			Message:  fmt.Sprintf("failed to serialize kustomization output: %v", err),
			Severity: "error",
		}}
	}

	return decodeActivityPolicies(yamlBytes, dir)
}

func loadPoliciesFromFile(filePath string) ([]LoadedPolicy, []ValidationIssue) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   filePath,
			Message:  fmt.Sprintf("failed to read file: %v", err),
			Severity: "error",
		}}
	}

	return decodeActivityPolicies(data, filePath)
}

func decodeActivityPolicies(data []byte, source string) ([]LoadedPolicy, []ValidationIssue) {
	var policies []LoadedPolicy
	var issues []ValidationIssue

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			issues = append(issues, ValidationIssue{
				Source:   source,
				Message:  fmt.Sprintf("failed to read YAML document: %v", err),
				Severity: "error",
			})
			break
		}

		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		// Skip comment-only documents that decode to nil.
		var raw any
		if err := sigsyaml.Unmarshal(doc, &raw); err != nil {
			issues = append(issues, ValidationIssue{
				Source:   source,
				Message:  fmt.Sprintf("invalid YAML: %v", err),
				Severity: "error",
			})
			continue
		}
		if raw == nil {
			continue
		}

		var typeMeta metav1.TypeMeta
		if err := sigsyaml.Unmarshal(doc, &typeMeta); err != nil {
			issues = append(issues, ValidationIssue{
				Source:   source,
				Message:  fmt.Sprintf("failed to decode resource metadata: %v", err),
				Severity: "error",
			})
			continue
		}

		if typeMeta.Kind != "ActivityPolicy" {
			continue
		}

		var metaOnly struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		_ = sigsyaml.Unmarshal(doc, &metaOnly)

		if typeMeta.APIVersion != activityPolicyAPIVersion {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: metaOnly.Metadata.Name,
				Field:      "apiVersion",
				Message:    fmt.Sprintf("unsupported apiVersion %q for ActivityPolicy (expected %q)", typeMeta.APIVersion, activityPolicyAPIVersion),
				Severity:   "error",
			})
			continue
		}

		var policy v1alpha1.ActivityPolicy
		if err := sigsyaml.UnmarshalStrict(doc, &policy); err != nil {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: metaOnly.Metadata.Name,
				Message:    fmt.Sprintf("invalid ActivityPolicy schema: %v", err),
				Severity:   "error",
			})
			continue
		}

		policies = append(policies, LoadedPolicy{
			Policy: &policy,
			Source: source,
		})
	}

	return policies, issues
}
