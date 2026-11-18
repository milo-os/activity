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
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/kustomize/api/krusty"
	kusttypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	sigsyaml "sigs.k8s.io/yaml"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	activityPolicyKind       = "ActivityPolicy"
	activityGroup            = "activity.miloapis.com"
	activityPolicyAPIVersion = activityGroup + "/v1alpha1"
)

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

	add := func(f string) {
		abs := absPath(f)
		if _, ok := seen[abs]; !ok {
			seen[abs] = struct{}{}
			files = append(files, f)
		}
	}

	for _, p := range paths {
		cleanPath := filepath.Clean(p)
		info, err := os.Stat(cleanPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}

		if !info.IsDir() {
			add(cleanPath)
			continue
		}

		var dirFiles []string
		err = filepath.WalkDir(cleanPath, func(entryPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !d.IsDir() && IsTestFile(entryPath) {
				dirFiles = append(dirFiles, entryPath)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", p, err)
		}
		sort.Strings(dirFiles)
		for _, f := range dirFiles {
			add(f)
		}
	}

	return files, nil
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

// loadUnit is a file or kustomization directory to load policies from.
type loadUnit struct {
	path          string
	kustomization bool
	// explicit is true when the user named this file directly.
	explicit bool
}

// LoadPoliciesFromPaths loads ActivityPolicy resources from files, directories,
// or Kustomize directories/components. Non-ActivityPolicy manifests inside a
// kustomization are ignored; suspicious kinds produce warnings. Files that a
// rendered kustomization uses as patches are not loaded as standalone policies.
func LoadPoliciesFromPaths(paths []string) ([]LoadedPolicy, []ValidationIssue) {
	var issues []ValidationIssue
	var units []loadUnit

	for _, p := range paths {
		u, unitIssues := collectLoadUnits(filepath.Clean(p))
		units = append(units, u...)
		issues = append(issues, unitIssues...)
	}

	// Scan every kustomization first so patch files can be excluded no matter
	// which order paths were given in.
	patchOwners := make(map[string]string)
	scans := make(map[string]*kustomizationScan)
	for _, u := range units {
		if !u.kustomization {
			continue
		}
		scan := scanKustomization(u.path)
		scans[u.path] = scan
		for patch, owner := range scan.patches {
			if _, ok := patchOwners[patch]; !ok {
				patchOwners[patch] = owner
			}
		}
	}

	var policies []LoadedPolicy
	for _, u := range units {
		if u.kustomization {
			scan := scans[u.path]
			if len(scan.issues) > 0 {
				issues = append(issues, scan.issues...)
				continue
			}
			loaded, loadIssues := loadPoliciesFromKustomization(u.path)
			policies = append(policies, loaded...)
			issues = append(issues, loadIssues...)
			continue
		}

		if owner, ok := patchOwners[absPath(u.path)]; ok {
			if u.explicit {
				issues = append(issues, ValidationIssue{
					Source:   u.path,
					Message:  fmt.Sprintf("file is a kustomize patch used by %s; not loaded as a standalone policy", owner),
					Severity: SeverityWarning,
				})
			}
			continue
		}

		loaded, loadIssues := loadPoliciesFromFile(u.path)
		policies = append(policies, loaded...)
		issues = append(issues, loadIssues...)
	}

	return policies, issues
}

func collectLoadUnits(path string) ([]loadUnit, []ValidationIssue) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   path,
			Message:  fmt.Sprintf("failed to access path: %v", err),
			Severity: SeverityError,
		}}
	}

	if !info.IsDir() {
		return []loadUnit{{path: path, explicit: true}}, nil
	}

	if hasKustomizationFile(path) {
		return []loadUnit{{path: path, kustomization: true}}, nil
	}

	var units []loadUnit
	var issues []ValidationIssue
	err = filepath.WalkDir(path, func(entryPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			issues = append(issues, ValidationIssue{
				Source:   entryPath,
				Message:  fmt.Sprintf("failed to read directory entry: %v", walkErr),
				Severity: SeverityError,
			})
			return nil
		}

		if d.IsDir() {
			if entryPath != path && hasKustomizationFile(entryPath) {
				units = append(units, loadUnit{path: entryPath, kustomization: true})
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
		units = append(units, loadUnit{path: entryPath})
		return nil
	})
	if err != nil {
		issues = append(issues, ValidationIssue{
			Source:   path,
			Message:  fmt.Sprintf("failed to walk directory: %v", err),
			Severity: SeverityError,
		})
	}
	return units, issues
}

func hasKustomizationFile(dir string) bool {
	return kustomizationFile(dir) != ""
}

func kustomizationFile(dir string) string {
	for _, name := range kustomizeFileNames {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func isKustomizationFilename(name string) bool {
	for _, candidate := range kustomizeFileNames {
		if name == candidate {
			return true
		}
	}
	return false
}

// kustomizationScan is the result of statically walking a kustomization tree
// before rendering it.
type kustomizationScan struct {
	// patches maps absolute patch file paths to the kustomization using them.
	patches map[string]string
	// issues are problems that prevent an offline render (remote bases,
	// missing local resources, unreadable kustomization files).
	issues []ValidationIssue
}

// scanKustomization walks a kustomization and the local kustomizations it
// references, collecting patch files and rejecting anything that would make
// kustomize reach the network.
func scanKustomization(root string) *kustomizationScan {
	scan := &kustomizationScan{patches: make(map[string]string)}
	visited := make(map[string]bool)

	var walk func(dir string)
	walk = func(dir string) {
		abs := absPath(dir)
		if visited[abs] {
			return
		}
		visited[abs] = true

		kfile := kustomizationFile(dir)
		if kfile == "" {
			return
		}
		data, err := os.ReadFile(kfile)
		if err != nil {
			scan.issues = append(scan.issues, ValidationIssue{
				Source:   kfile,
				Message:  fmt.Sprintf("failed to read kustomization: %v", err),
				Severity: SeverityError,
			})
			return
		}
		var k kusttypes.Kustomization
		if err := sigsyaml.Unmarshal(data, &k); err != nil {
			scan.issues = append(scan.issues, ValidationIssue{
				Source:   kfile,
				Message:  fmt.Sprintf("failed to build kustomization: invalid kustomization file: %v", err),
				Severity: SeverityError,
			})
			return
		}

		addPatch := func(rel string) {
			if rel == "" || strings.Contains(rel, "\n") {
				return
			}
			p := filepath.Join(dir, rel)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				scan.patches[absPath(p)] = kfile
			}
		}
		for _, p := range k.Patches {
			addPatch(p.Path)
		}
		for _, p := range k.PatchesJson6902 {
			addPatch(p.Path)
		}
		for _, p := range k.PatchesStrategicMerge {
			addPatch(string(p))
		}

		refs := make([]string, 0, len(k.Resources)+len(k.Components)+len(k.Bases))
		refs = append(refs, k.Resources...)
		refs = append(refs, k.Components...)
		refs = append(refs, k.Bases...)
		for _, ref := range refs {
			if isRemoteRef(ref) {
				scan.issues = append(scan.issues, ValidationIssue{
					Source:   kfile,
					Message:  fmt.Sprintf("offline: remote bases not supported: %q", ref),
					Severity: SeverityError,
				})
				continue
			}
			p := filepath.Join(dir, ref)
			info, err := os.Stat(p)
			if err != nil {
				// Kustomize would try a missing local path as a remote
				// repository, so stop here instead of letting it fetch.
				scan.issues = append(scan.issues, ValidationIssue{
					Source:   kfile,
					Message:  fmt.Sprintf("failed to build kustomization: resource %q not found (remote bases are not supported offline)", ref),
					Severity: SeverityError,
				})
				continue
			}
			if info.IsDir() {
				walk(p)
			}
		}
	}

	walk(root)
	return scan
}

// isRemoteRef reports whether a kustomization resource entry points at a
// remote location (git repository or URL).
func isRemoteRef(ref string) bool {
	lower := strings.ToLower(ref)
	for _, prefix := range []string{"git@", "git::", "github.com/", "gitlab.com/", "bitbucket.org/", "ssh:", "file:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(lower, "://") || strings.Contains(lower, "?ref=") || strings.Contains(lower, "?version=")
}

func loadPoliciesFromKustomization(dir string) ([]LoadedPolicy, []ValidationIssue) {
	// Match `kustomize build` defaults: files must live under the
	// kustomization root, and plugins are disabled.
	opts := &krusty.Options{
		LoadRestrictions: kusttypes.LoadRestrictionsRootOnly,
		PluginConfig:     kusttypes.DisabledPluginConfig(),
	}
	k := krusty.MakeKustomizer(opts)
	resMap, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   dir,
			Message:  fmt.Sprintf("failed to build kustomization: %v", err),
			Severity: SeverityError,
		}}
	}

	yamlBytes, err := resMap.AsYaml()
	if err != nil {
		return nil, []ValidationIssue{{
			Source:   dir,
			Message:  fmt.Sprintf("failed to serialize kustomization output: %v", err),
			Severity: SeverityError,
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
			Severity: SeverityError,
		}}
	}

	return decodeActivityPolicies(data, filePath)
}

// splitYAMLDocuments splits a multi-document YAML stream. Callers report
// 1-based positions in the returned slice; empty documents between separators
// are kept so indexes match what a reader counts (a leading "---" does not
// start a new document).
func splitYAMLDocuments(data []byte) ([][]byte, error) {
	var docs [][]byte
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return docs, nil
			}
			return docs, err
		}
		docs = append(docs, doc)
	}
}

func decodeActivityPolicies(data []byte, source string) ([]LoadedPolicy, []ValidationIssue) {
	var policies []LoadedPolicy
	var issues []ValidationIssue

	docs, err := splitYAMLDocuments(data)
	if err != nil {
		issues = append(issues, ValidationIssue{
			Source:   source,
			Document: len(docs) + 1,
			Message:  fmt.Sprintf("failed to read YAML document: %v", err),
			Severity: SeverityError,
		})
	}

	for i, doc := range docs {
		docIndex := i + 1
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		// Skip comment-only documents that decode to nil.
		var raw any
		if err := sigsyaml.Unmarshal(doc, &raw); err != nil {
			issues = append(issues, ValidationIssue{
				Source:   source,
				Document: docIndex,
				Message:  fmt.Sprintf("invalid YAML: %v", err),
				Severity: SeverityError,
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
				Document: docIndex,
				Message:  fmt.Sprintf("failed to decode resource metadata: %v", err),
				Severity: SeverityError,
			})
			continue
		}

		var metaOnly struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		_ = sigsyaml.Unmarshal(doc, &metaOnly)

		if typeMeta.Kind != activityPolicyKind {
			if msg := unexpectedKindMessage(typeMeta); msg != "" {
				issues = append(issues, ValidationIssue{
					Source:     source,
					Document:   docIndex,
					PolicyName: metaOnly.Metadata.Name,
					Field:      "kind",
					Message:    msg,
					Severity:   SeverityWarning,
				})
			}
			continue
		}

		if typeMeta.APIVersion != activityPolicyAPIVersion {
			issues = append(issues, ValidationIssue{
				Source:     source,
				Document:   docIndex,
				PolicyName: metaOnly.Metadata.Name,
				Field:      "apiVersion",
				Message:    fmt.Sprintf("unsupported apiVersion %q for ActivityPolicy (expected %q)", typeMeta.APIVersion, activityPolicyAPIVersion),
				Severity:   SeverityError,
			})
			continue
		}

		var policy v1alpha1.ActivityPolicy
		if err := sigsyaml.UnmarshalStrict(doc, &policy); err != nil {
			issues = append(issues, ValidationIssue{
				Source:     source,
				Document:   docIndex,
				PolicyName: metaOnly.Metadata.Name,
				Message:    fmt.Sprintf("invalid ActivityPolicy schema: %v", err),
				Severity:   SeverityError,
			})
			continue
		}

		policies = append(policies, LoadedPolicy{
			Policy:   &policy,
			Source:   source,
			Document: docIndex,
		})
	}

	return policies, issues
}

// unexpectedKindMessage returns a warning for documents that look like they
// were meant to be ActivityPolicies but will be ignored, or "" for unrelated
// manifests.
func unexpectedKindMessage(tm metav1.TypeMeta) string {
	if strings.EqualFold(tm.Kind, activityPolicyKind) {
		return fmt.Sprintf("kind %q is not %q (kinds are case-sensitive); document ignored", tm.Kind, activityPolicyKind)
	}
	gv, err := schema.ParseGroupVersion(tm.APIVersion)
	if err == nil && gv.Group == activityGroup {
		return fmt.Sprintf("unexpected kind %q for apiVersion %q (expected %q); document ignored", tm.Kind, tm.APIVersion, activityPolicyKind)
	}
	return ""
}
