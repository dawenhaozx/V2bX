package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	osExec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/InazumaV/V2bX/common/exec"
	"github.com/spf13/cobra"
)

var targetVersion string
var runUninstallCommand = exec.RunCommandByShell

var (
	updateCommand = cobra.Command{
		Use:   "update",
		Short: "Update V2bX version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate V2bX executable: %w", err)
			}
			client := &http.Client{Timeout: 2 * time.Minute}
			return downloadAndRunInstaller(cmd.Context(), targetVersion, filepath.Dir(executable), client, runInstaller)
		},
		Args: cobra.NoArgs,
	}
	uninstallCommand = cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall V2bX",
		Run:   uninstallHandle,
	}
)

const installerRepository = "https://github.com/dawenhaozx/V2bX"

var releaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func downloadAndRunInstaller(ctx context.Context, version, installDir string, client *http.Client, run func(context.Context, string, ...string) error) error {
	if version == "" {
		var err error
		version, err = latestReleaseVersion(ctx, client)
		if err != nil {
			return err
		}
	} else {
		if !releaseVersionPattern.MatchString(version) {
			return fmt.Errorf("invalid release version %q", version)
		}
	}
	installerURL := installerRepository + "/releases/download/" + version + "/install.sh"
	args := []string{"--install-dir", installDir, "--version", version}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, installerURL, nil)
	if err != nil {
		return fmt.Errorf("create installer download request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download V2bX installer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download V2bX installer: HTTP %d", response.StatusCode)
	}
	const maxInstallerSize = 1024 * 1024
	content, err := io.ReadAll(io.LimitReader(response.Body, maxInstallerSize+1))
	if err != nil {
		return fmt.Errorf("read V2bX installer: %w", err)
	}
	if len(content) > maxInstallerSize || !strings.HasPrefix(string(content), "#!") {
		return fmt.Errorf("downloaded V2bX installer is not a valid shell script")
	}
	script, err := os.CreateTemp("", "V2bX-install-*.sh")
	if err != nil {
		return fmt.Errorf("create temporary installer: %w", err)
	}
	defer os.Remove(script.Name())
	if _, err = script.Write(content); err != nil {
		script.Close()
		return fmt.Errorf("write temporary installer: %w", err)
	}
	if err = script.Close(); err != nil {
		return fmt.Errorf("close temporary installer: %w", err)
	}
	if err = run(ctx, script.Name(), args...); err != nil {
		return fmt.Errorf("run V2bX installer: %w", err)
	}
	return nil
}

func latestReleaseVersion(ctx context.Context, client *http.Client) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, installerRepository+"/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("create latest release request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("resolve latest V2bX release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request == nil {
		return "", fmt.Errorf("resolve latest V2bX release: unexpected HTTP response %d", response.StatusCode)
	}
	const tagPrefix = "/dawenhaozx/V2bX/releases/tag/"
	finalURL := response.Request.URL
	if finalURL.Scheme != "https" || finalURL.Host != "github.com" || !strings.HasPrefix(finalURL.Path, tagPrefix) {
		return "", fmt.Errorf("resolve latest V2bX release: expected a GitHub release tag")
	}
	version := strings.TrimPrefix(finalURL.Path, tagPrefix)
	if !releaseVersionPattern.MatchString(version) {
		return "", fmt.Errorf("resolve latest V2bX release: invalid version %q", version)
	}
	return version, nil
}

func runInstaller(ctx context.Context, script string, args ...string) error {
	process := osExec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	return process.Run()
}

func init() {
	updateCommand.PersistentFlags().StringVar(&targetVersion, "version", "", "update target version")
	command.AddCommand(&updateCommand)
	command.AddCommand(&uninstallCommand)
}

func uninstallHandle(_ *cobra.Command, _ []string) {
	var yes string
	fmt.Println(Warn("确定要卸载 V2bX 吗?(Y/n)"))
	fmt.Scan(&yes)
	if strings.ToLower(yes) != "y" {
		fmt.Println("已取消卸载")
		return
	}
	_, err := runUninstallCommand("systemctl stop V2bX&&systemctl disable V2bX")
	if err != nil {
		fmt.Println(Err("exec cmd error: ", err))
		fmt.Println(Err("卸载失败"))
		return
	}
	_ = os.RemoveAll("/etc/systemd/system/V2bX.service")
	_ = os.RemoveAll("/etc/V2bX/")
	_ = os.RemoveAll("/usr/local/V2bX/")
	_ = os.RemoveAll("/bin/V2bX")
	if info, err := os.Lstat("/usr/local/bin/V2bX"); err == nil && info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove("/usr/local/bin/V2bX")
	}
	_, err = runUninstallCommand("systemctl daemon-reload&&systemctl reset-failed")
	if err != nil {
		fmt.Println(Err("exec cmd error: ", err))
		fmt.Println(Err("卸载失败"))
		return
	}
	fmt.Println(Ok("卸载成功"))
}
