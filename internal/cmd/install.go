package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/mholt/archiver/v3"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/toolctl/toolctl/internal/api"
	"github.com/toolctl/toolctl/internal/utils"
	"golang.org/x/sys/unix"
)

func newInstallCmd(
	toolctlWriter io.Writer, localAPIFS afero.Fs,
) *cobra.Command {
	var installCmd = &cobra.Command{
		Use:   "install TOOL[@VERSION]... [flags]",
		Short: "Install one or more tools",
		Example: `  # Install the latest version of a tool
  toolctl install minikube

  # Install a specified version of a tool
  toolctl install kubectl@1.20.13

  # Install multiple tools
  toolctl install kustomize k9s`,
		Args: checkArgs(),
		RunE: newRunInstall(toolctlWriter, localAPIFS),
	}
	return installCmd
}

func newRunInstall(
	toolctlWriter io.Writer, localAPIFS afero.Fs,
) func(cmd *cobra.Command, args []string) (err error) {
	return func(cmd *cobra.Command, args []string) (err error) {
		allTools, err := ArgsToTools(args, true)
		if err != nil {
			return err
		}

		// Check if the install directory exists
		installDir, err := utils.RequireConfigString("InstallDir")
		if err != nil {
			return
		}
		_, err = os.Stat(installDir)
		if err != nil {
			err = fmt.Errorf(
				"install directory %s does not exist",
				wrapInQuotesIfContainsSpace(installDir),
			)
			return
		}

		// Check if we have write access to the install directory
		if unix.Access(installDir, unix.W_OK) != nil {
			var currentUser *user.User
			currentUser, err = user.Current()
			if err != nil {
				return
			}
			err = fmt.Errorf("%s is not writable by user %s, try running:\n  sudo toolctl install %s",
				wrapInQuotesIfContainsSpace(installDir),
				currentUser.Username, toolsToArgs(allTools),
			)
			return
		}

		// Check if the install directory is in the PATH
		var installDirInPath bool
		pathEnv := os.Getenv("PATH")
		paths := strings.Split(pathEnv, ":")
		for _, path := range paths {
			if path == installDir {
				installDirInPath = true
				break
			}
		}
		if !installDirInPath {
			fmt.Fprintf(
				toolctlWriter,
				"🚨 %s is not in $PATH\n",
				wrapInQuotesIfContainsSpace(installDir),
			)
		}

		toolctlAPI, err := api.New(localAPIFS, cmd, api.Remote)
		if err != nil {
			return err
		}

		for _, tool := range allTools {
			err = install(toolctlWriter, toolctlAPI, installDir, tool, allTools)
			if err != nil {
				return
			}
		}

		return
	}
}

func install(
	toolctlWriter io.Writer, toolctlAPI api.ToolctlAPI, installDir string,
	tool api.Tool, allTools []api.Tool,
) (err error) {

	// Check if the tool is supported
	toolMeta, err := api.GetToolMeta(toolctlAPI, tool)
	if err != nil {
		return
	}

	// Check if a version has been specified
	latestVersion, err := api.GetLatestVersion(toolctlAPI, tool)
	if err != nil {
		return
	}
	if tool.Version == "" {
		tool.Version = latestVersion.String()
	}

	// Check if the tool is already installed
	installedToolPath, err := which(tool.Name)
	if err != nil {
		return
	}

	if installedToolPath != "" {
		err = infoPrintInstalledVersion(
			installedToolPath, toolMeta, toolctlWriter, tool, allTools, latestVersion,
		)
		return
	}

	fmt.Fprintln(
		toolctlWriter,
		prependToolName(tool, allTools, fmt.Sprintf(
			"👷 Installing v%s ...", tool.Version),
		),
	)

	// Download the tool to a temporary directory
	tempDir, err := ioutil.TempDir("", "toolctl-install-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tempDir)
	downloadedFilePath, err := downloadTool(tempDir, toolctlAPI, tool)
	if err != nil {
		return
	}

	// Extract the tool if it's a .tar.gz file
	var extractedBinaryPath string
	if strings.HasSuffix(downloadedFilePath, ".tar.gz") {
		// Extract the downloaded file to a temporary directory
		err = archiver.Unarchive(downloadedFilePath, tempDir)
		if err != nil {
			return
		}
		// Locate the extracted binary
		extractedBinaryPath, err = locateExtractedBinary(downloadedFilePath, tool)
		if err != nil {
			return
		}
	} else {
		extractedBinaryPath = downloadedFilePath
	}

	// Make the binary executable
	err = os.Chmod(extractedBinaryPath, 0755)
	if err != nil {
		return
	}

	// Install the binary
	installPath := filepath.Join(installDir, tool.Name)
	err = os.Rename(extractedBinaryPath, installPath)
	if err != nil {
		return
	}

	installedVersion, err := getInstalledVersion(
		installPath, toolMeta.VersionArgs,
	)
	if err != nil {
		return
	}

	if !installedVersion.Equal(semver.MustParse(tool.Version)) {
		err = fmt.Errorf(
			"installation failed: Expected v%s, but installed v%s",
			tool.Version, installedVersion.String(),
		)
		return
	}

	fmt.Fprintln(
		toolctlWriter,
		prependToolName(tool, allTools, "🎉 Successfully installed"),
	)

	return
}

func infoPrintInstalledVersion(
	installedToolPath string, toolMeta api.ToolMeta, toolctlWriter io.Writer,
	tool api.Tool, allTools []api.Tool, latestVersion *semver.Version,
) (err error) {
	var installedVersion *semver.Version
	installedVersion, err = getInstalledVersion(
		installedToolPath, toolMeta.VersionArgs,
	)
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return
		}

		fmt.Fprintln(
			toolctlWriter,
			prependToolName(
				tool, allTools, "🤷 Unknown version is already installed",
			),
		)
		fmt.Fprintln(
			toolctlWriter,
			prependToolName(
				tool, allTools, "💁 For more details run: toolctl info "+tool.Name,
			),
		)
		err = nil
		return
	}

	installedVersionString := installedVersion.String()
	if installedVersion.Equal(latestVersion) {
		installedVersionString += " (the latest version)"
	}
	fmt.Fprintln(
		toolctlWriter,
		prependToolName(tool, allTools, fmt.Sprintf(
			"🤷 v%s is already installed", installedVersionString),
		),
	)

	fmt.Fprintln(
		toolctlWriter,
		prependToolName(
			tool, allTools, "💁 For more details run: toolctl info "+tool.Name,
		),
	)

	return
}

func locateExtractedBinary(
	downloadedFilePath string, tool api.Tool,
) (extractedBinaryPath string, err error) {
	// The binary has been extracted to a subdirectory.
	// Example: https://github.com/derailed/k9s/releases/download/v0.25.7/k9s_Darwin_x86_64.tar.gz
	extractedBinaryPath = filepath.Join(
		strings.Replace(downloadedFilePath, ".tar.gz", "", 1), tool.Name,
	)
	_, err = os.Stat(extractedBinaryPath)
	if err == nil {
		return
	}

	// The binary has been extracted directly to the directory.
	// Example: https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.4.1/kustomize_v4.4.1_darwin_amd64.tar.gz
	extractedBinaryPath = filepath.Join(
		path.Dir(downloadedFilePath), tool.Name,
	)
	_, err = os.Stat(extractedBinaryPath)
	if err == nil {
		return
	}

	err = fmt.Errorf(
		"failed to locate extracted binary for %s",
		tool.Name,
	)
	return
}

// downloadTool gets the download URL for the specified tool and downloads it
// to the specified path.
func downloadTool(dir string, toolctlAPI api.ToolctlAPI, tool api.Tool) (filePath string, err error) {
	meta, err := api.GetToolPlatformVersionMeta(toolctlAPI, tool)
	if err != nil {
		return
	}
	expectedSHA256 := meta.SHA256

	resp, err := http.Get(meta.URL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		return
	}

	// Create the file
	out, err := os.Create(filepath.Join(dir, path.Base(meta.URL)))
	if err != nil {
		return
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)

	// Check the SHA256 hash
	downloadedFile, err := os.Open(out.Name())
	if err != nil {
		return
	}
	calculatedSHA256, err := CalculateSHA256(downloadedFile)
	if err != nil {
		return
	}

	if calculatedSHA256 != expectedSHA256 {
		err = fmt.Errorf("SHA256 hash mismatch, wanted %s, got %s", expectedSHA256, calculatedSHA256)
		return
	}

	filePath = out.Name()

	return
}
