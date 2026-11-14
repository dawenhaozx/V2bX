package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

type installerRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip installerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestDownloadAndRunInstaller(t *testing.T) {
	for _, test := range []struct {
		name, version, path, latestTag string
		args                           []string
	}{
		{"latest", "", "/releases/download/v9.8.7/install.sh", "v9.8.7", []string{"--install-dir", "/custom/V2bX", "--version", "v9.8.7"}},
		{"latest numeric tag", "", "/releases/download/9.8.7/install.sh", "9.8.7", []string{"--install-dir", "/custom/V2bX", "--version", "9.8.7"}},
		{"tag", "v1.2.3", "/releases/download/v1.2.3/install.sh", "", []string{"--install-dir", "/custom/V2bX", "--version", "v1.2.3"}},
		{"numeric", "1.2.3", "/releases/download/1.2.3/install.sh", "", []string{"--install-dir", "/custom/V2bX", "--version", "1.2.3"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const script = "#!/usr/bin/env bash\nexit 0\n"
			client := &http.Client{Transport: installerRoundTripper(func(request *http.Request) (*http.Response, error) {
				if test.latestTag != "" {
					switch request.URL.String() {
					case installerRepository + "/releases/latest":
						return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{installerRepository + "/releases/tag/" + test.latestTag}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
					case installerRepository + "/releases/tag/" + test.latestTag:
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("release page")), Request: request}, nil
					}
				}
				if got, want := request.URL.String(), installerRepository+test.path; got != want {
					t.Errorf("installer URL = %q, want %q", got, want)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(script)), Request: request}, nil
			})}
			var downloadedPath string
			runs := 0
			err := downloadAndRunInstaller(context.Background(), test.version, "/custom/V2bX", client, func(_ context.Context, path string, args ...string) error {
				runs++
				downloadedPath = path
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != script || !reflect.DeepEqual(args, test.args) {
					t.Errorf("unexpected installer content or arguments: %q, %q", content, args)
				}
				return nil
			})
			if err != nil || runs != 1 {
				t.Fatalf("update returned %v; runner calls = %d", err, runs)
			}
			if _, err := os.Stat(downloadedPath); !os.IsNotExist(err) {
				t.Errorf("temporary installer was not removed: %v", err)
			}
		})
	}
}

func TestDownloadAndRunInstallerFailures(t *testing.T) {
	installerErr := errors.New("installer failed")
	for _, test := range []struct {
		name, version, body string
		status              int
		downloadErr, runErr error
		wantRun             bool
	}{
		{name: "invalid version", version: "../bad", status: http.StatusOK, body: "#!/bin/bash\n"},
		{name: "network", version: "v1.2.3", downloadErr: errors.New("offline")},
		{name: "missing release", version: "v1.2.3", status: http.StatusNotFound, body: "Not Found"},
		{name: "invalid response", version: "v1.2.3", status: http.StatusOK, body: "<html>error</html>"},
		{name: "oversized", version: "v1.2.3", status: http.StatusOK, body: "#!" + strings.Repeat("x", 1024*1024)},
		{name: "installer exit", version: "v1.2.3", status: http.StatusOK, body: "#!/bin/bash\n", runErr: installerErr, wantRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: installerRoundTripper(func(*http.Request) (*http.Response, error) {
				if test.downloadErr != nil {
					return nil, test.downloadErr
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			ran := false
			err := downloadAndRunInstaller(context.Background(), test.version, "/custom/V2bX", client, func(context.Context, string, ...string) error {
				ran = true
				return test.runErr
			})
			if err == nil || ran != test.wantRun {
				t.Fatalf("error = %v, ran = %v, want failure and ran = %v", err, ran, test.wantRun)
			}
			if test.runErr != nil && !errors.Is(err, test.runErr) {
				t.Errorf("installer error was not preserved: %v", err)
			}
		})
	}
}

func TestCancelUninstallRunsNoCommands(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = writer.WriteString("n\n"); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	writer.Close()
	previousStdin, previousRunner := os.Stdin, runUninstallCommand
	defer func() { os.Stdin, runUninstallCommand = previousStdin, previousRunner }()
	os.Stdin = reader
	calls := 0
	runUninstallCommand = func(string) (string, error) {
		calls++
		return "", errors.New("system commands are forbidden in this test")
	}
	uninstallHandle(nil, nil)
	if calls != 0 {
		t.Fatalf("cancelled uninstall ran %d system commands", calls)
	}
}

func TestLatestReleaseVersionRejectsUnexpectedDestinations(t *testing.T) {
	for _, destination := range []string{
		installerRepository + "/releases",
		installerRepository + "/releases/tag/bad/version",
		"https://example.com/dawenhaozx/V2bX/releases/tag/v1.2.3",
	} {
		t.Run(destination, func(t *testing.T) {
			client := &http.Client{Transport: installerRoundTripper(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == installerRepository+"/releases/latest" {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			})}
			if version, err := latestReleaseVersion(context.Background(), client); err == nil {
				t.Errorf("unexpected release destination accepted as %q", version)
			}
		})
	}
}
