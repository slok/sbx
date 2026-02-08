package image

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

const (
	// DefaultRepo is the default GitHub repository for sbx images.
	DefaultRepo = "slok/sbx-images"
	// DefaultImagesDir is the default local images directory (relative to home).
	DefaultImagesDir = ".sbx/images"

	defaultGitHubAPIBase        = "https://api.github.com"
	defaultGitHubDownloadBase   = "https://github.com"
	defaultFirecrackerRepoOwner = "firecracker-microvm"
	defaultFirecrackerRepoName  = "firecracker"
)

// GitHubImageManagerConfig configures the GitHub-backed image manager.
type GitHubImageManagerConfig struct {
	// Repo is the GitHub repository (e.g. "slok/sbx-images").
	Repo string
	// ImagesDir is the local directory for storing images.
	ImagesDir string
	// HTTPClient is the HTTP client for API and download requests.
	HTTPClient *http.Client
	// Logger for logging.
	Logger log.Logger
}

func (c *GitHubImageManagerConfig) defaults() error {
	if c.Repo == "" {
		c.Repo = DefaultRepo
	}
	if c.ImagesDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not get user home dir: %w", err)
		}
		c.ImagesDir = filepath.Join(home, DefaultImagesDir)
	}
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	return nil
}

// GitHubImageManager implements ImageManager using GitHub Releases.
type GitHubImageManager struct {
	repo       string
	imagesDir  string
	httpClient *http.Client
	logger     log.Logger

	// Base URLs (overridable for testing).
	apiBaseURL      string
	downloadBaseURL string
}

// NewGitHubImageManager creates a new GitHub-backed image manager.
func NewGitHubImageManager(cfg GitHubImageManagerConfig) (*GitHubImageManager, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &GitHubImageManager{
		repo:            cfg.Repo,
		imagesDir:       cfg.ImagesDir,
		httpClient:      cfg.HTTPClient,
		logger:          cfg.Logger,
		apiBaseURL:      defaultGitHubAPIBase,
		downloadBaseURL: defaultGitHubDownloadBase,
	}, nil
}

// NewGitHubImageManagerWithBaseURL creates a manager with custom base URLs (for testing).
func NewGitHubImageManagerWithBaseURL(cfg GitHubImageManagerConfig, apiBaseURL, downloadBaseURL string) (*GitHubImageManager, error) {
	m, err := NewGitHubImageManager(cfg)
	if err != nil {
		return nil, err
	}
	m.apiBaseURL = apiBaseURL
	m.downloadBaseURL = downloadBaseURL
	return m, nil
}

// --- JSON wire types (private, for GitHub API and manifest parsing) ---

type ghRelease struct {
	TagName string `json:"tag_name"`
}

type manifestJSON struct {
	SchemaVersion int                          `json:"schema_version"`
	Version       string                       `json:"version"`
	Artifacts     map[string]archArtifactsJSON `json:"artifacts"`
	FC            firecrackerJSON              `json:"firecracker"`
	Build         buildJSON                    `json:"build"`
}

type archArtifactsJSON struct {
	Kernel kernelJSON `json:"kernel"`
	Rootfs rootfsJSON `json:"rootfs"`
}

type kernelJSON struct {
	File      string `json:"file"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	SizeBytes int64  `json:"size_bytes"`
}

type rootfsJSON struct {
	File          string `json:"file"`
	Distro        string `json:"distro"`
	DistroVersion string `json:"distro_version"`
	Profile       string `json:"profile"`
	SizeBytes     int64  `json:"size_bytes"`
}

type firecrackerJSON struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

type buildJSON struct {
	Date   string `json:"date"`
	Commit string `json:"commit"`
}

func (m *manifestJSON) toModel() *model.ImageManifest {
	artifacts := make(map[string]model.ArchArtifacts, len(m.Artifacts))
	for arch, a := range m.Artifacts {
		artifacts[arch] = model.ArchArtifacts{
			Kernel: model.KernelInfo{
				File:      a.Kernel.File,
				Version:   a.Kernel.Version,
				Source:    a.Kernel.Source,
				SizeBytes: a.Kernel.SizeBytes,
			},
			Rootfs: model.RootfsInfo{
				File:          a.Rootfs.File,
				Distro:        a.Rootfs.Distro,
				DistroVersion: a.Rootfs.DistroVersion,
				Profile:       a.Rootfs.Profile,
				SizeBytes:     a.Rootfs.SizeBytes,
			},
		}
	}
	return &model.ImageManifest{
		SchemaVersion: m.SchemaVersion,
		Version:       m.Version,
		Artifacts:     artifacts,
		Firecracker: model.FirecrackerInfo{
			Version: m.FC.Version,
			Source:  m.FC.Source,
		},
		Build: model.BuildInfo{
			Date:   m.Build.Date,
			Commit: m.Build.Commit,
		},
	}
}

// --- ImageManager interface implementation ---

func (g *GitHubImageManager) ListReleases(ctx context.Context) ([]model.ImageRelease, error) {
	releases, err := g.fetchReleases(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching releases: %w", err)
	}

	result := make([]model.ImageRelease, 0, len(releases))
	for _, r := range releases {
		installed, _ := g.Exists(ctx, r.TagName)
		result = append(result, model.ImageRelease{
			Version:   r.TagName,
			Installed: installed,
		})
	}

	return result, nil
}

func (g *GitHubImageManager) GetManifest(ctx context.Context, version string) (*model.ImageManifest, error) {
	url := fmt.Sprintf("%s/%s/releases/download/%s/manifest.json", g.downloadBaseURL, g.repo, version)

	data, err := g.httpGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("downloading manifest for %s: %w", version, err)
	}

	var mj manifestJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("parsing manifest for %s: %w", version, err)
	}

	// Validate schema version. A schema_version of 0 means the field was absent
	// (pre-versioning manifests), which we treat as schema version 1 for backward compatibility.
	if mj.SchemaVersion == 0 {
		mj.SchemaVersion = 1
	}
	if mj.SchemaVersion != model.CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported manifest schema version %d for %s (supported: %d), try updating sbx",
			mj.SchemaVersion, version, model.CurrentSchemaVersion)
	}

	return mj.toModel(), nil
}

func (g *GitHubImageManager) Pull(ctx context.Context, version string, opts PullOptions) (*PullResult, error) {
	arch := HostArch()

	// Check if already installed.
	if !opts.Force {
		exists, _ := g.Exists(ctx, version)
		if exists {
			return &PullResult{
				Version:         version,
				Skipped:         true,
				KernelPath:      g.KernelPath(version),
				RootFSPath:      g.RootFSPath(version),
				FirecrackerPath: g.FirecrackerPath(version),
			}, nil
		}
	}

	// Fetch manifest to get artifact details and firecracker version.
	manifest, err := g.GetManifest(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("getting manifest: %w", err)
	}

	archArtifacts, ok := manifest.Artifacts[arch]
	if !ok {
		return nil, fmt.Errorf("no artifacts for architecture %q in release %s", arch, version)
	}

	// Create version directory.
	versionDir := filepath.Join(g.imagesDir, version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating version directory: %w", err)
	}

	// Cleanup on error.
	success := false
	defer func() {
		if !success {
			os.RemoveAll(versionDir)
		}
	}()

	// Download kernel.
	kernelURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", g.downloadBaseURL, g.repo, version, archArtifacts.Kernel.File)
	kernelPath := filepath.Join(versionDir, archArtifacts.Kernel.File)
	g.logger.Infof("Downloading kernel: %s", archArtifacts.Kernel.File)
	if err := g.downloadFile(ctx, kernelURL, kernelPath, archArtifacts.Kernel.SizeBytes, opts.StatusWriter); err != nil {
		return nil, fmt.Errorf("downloading kernel: %w", err)
	}

	// Download rootfs.
	rootfsURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", g.downloadBaseURL, g.repo, version, archArtifacts.Rootfs.File)
	rootfsPath := filepath.Join(versionDir, archArtifacts.Rootfs.File)
	g.logger.Infof("Downloading rootfs: %s", archArtifacts.Rootfs.File)
	if err := g.downloadFile(ctx, rootfsURL, rootfsPath, archArtifacts.Rootfs.SizeBytes, opts.StatusWriter); err != nil {
		return nil, fmt.Errorf("downloading rootfs: %w", err)
	}

	// Download and extract firecracker binary.
	fcPath := filepath.Join(versionDir, "firecracker")
	g.logger.Infof("Downloading Firecracker %s", manifest.Firecracker.Version)
	if err := g.downloadFirecracker(ctx, manifest.Firecracker.Version, arch, fcPath, opts.StatusWriter); err != nil {
		return nil, fmt.Errorf("downloading firecracker: %w", err)
	}

	success = true
	return &PullResult{
		Version:         version,
		Skipped:         false,
		KernelPath:      kernelPath,
		RootFSPath:      rootfsPath,
		FirecrackerPath: fcPath,
	}, nil
}

func (g *GitHubImageManager) Remove(_ context.Context, version string) error {
	versionDir := filepath.Join(g.imagesDir, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("image %s is not installed", version)
	}
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("removing image %s: %w", version, err)
	}
	return nil
}

func (g *GitHubImageManager) Exists(_ context.Context, version string) (bool, error) {
	versionDir := filepath.Join(g.imagesDir, version)
	info, err := os.Stat(versionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func (g *GitHubImageManager) KernelPath(version string) string {
	return filepath.Join(g.imagesDir, version, fmt.Sprintf("vmlinux-%s", HostArch()))
}

func (g *GitHubImageManager) RootFSPath(version string) string {
	return filepath.Join(g.imagesDir, version, fmt.Sprintf("rootfs-%s.ext4", HostArch()))
}

func (g *GitHubImageManager) FirecrackerPath(version string) string {
	return filepath.Join(g.imagesDir, version, "firecracker")
}

// --- Internal helpers ---

func (g *GitHubImageManager) fetchReleases(ctx context.Context) ([]ghRelease, error) {
	var allReleases []ghRelease
	page := 1
	for {
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", g.apiBaseURL, g.repo, page)
		data, err := g.httpGet(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("fetching releases page %d: %w", page, err)
		}

		var releases []ghRelease
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, fmt.Errorf("parsing releases page %d: %w", page, err)
		}

		if len(releases) == 0 {
			break
		}

		allReleases = append(allReleases, releases...)
		page++
	}

	return allReleases, nil
}

func (g *GitHubImageManager) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

func (g *GitHubImageManager) downloadFile(ctx context.Context, url, dstPath string, totalSize int64, statusWriter io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", dstPath, err)
	}
	defer f.Close()

	var dst io.Writer = f
	if statusWriter != nil {
		pw := NewProgressWriter(f, statusWriter, totalSize)
		defer pw.Finish()
		dst = pw
	}

	if _, err := io.Copy(dst, resp.Body); err != nil {
		os.Remove(dstPath)
		return fmt.Errorf("writing file %s: %w", dstPath, err)
	}

	return nil
}

func (g *GitHubImageManager) downloadFirecracker(ctx context.Context, fcVersion, arch, dstPath string, statusWriter io.Writer) error {
	// Download tgz from upstream Firecracker releases.
	tgzURL := fmt.Sprintf("%s/%s/%s/releases/download/%s/firecracker-%s-%s.tgz",
		g.downloadBaseURL, defaultFirecrackerRepoOwner, defaultFirecrackerRepoName, fcVersion, fcVersion, arch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tgzURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, tgzURL)
	}

	var reader io.Reader = resp.Body
	if statusWriter != nil {
		pw := NewProgressWriter(io.Discard, statusWriter, resp.ContentLength)
		reader = io.TeeReader(resp.Body, pw)
		defer pw.Finish()
	}

	// Extract the firecracker binary from the tgz.
	// Expected path: release-{version}-{arch}/firecracker-{version}-{arch}
	targetName := fmt.Sprintf("firecracker-%s-%s", fcVersion, arch)

	gz, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("decompressing tgz: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("firecracker binary %q not found in archive", targetName)
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Match by base name (ignores the directory prefix).
		if filepath.Base(header.Name) != targetName {
			continue
		}

		if !strings.HasSuffix(header.Name, ".debug") && header.Typeflag == tar.TypeReg {
			f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return fmt.Errorf("creating firecracker binary: %w", err)
			}
			defer f.Close()

			if _, err := io.Copy(f, tr); err != nil {
				os.Remove(dstPath)
				return fmt.Errorf("extracting firecracker binary: %w", err)
			}
			return nil
		}
	}
}
