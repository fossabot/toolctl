package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

var (
	shortFlag    bool
	gitVersion   = "v0.0.0-dev"
	gitCommit    = "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	gitTreeState = "unknown"
	buildDate    = "0000-00-00T00:00:00Z"
)

type VersionInfo struct {
	GitVersion   string
	GitCommit    string
	GitTreeState string
	BuildDate    string
}

func newVersionCmd(toolctlWriter io.Writer) *cobra.Command {
	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display the version of toolctl",
		Args:  cobra.NoArgs,
		RunE:  newRunVersion(toolctlWriter),
	}

	versionCmd.SetOut(toolctlWriter)
	versionCmd.SetErr(toolctlWriter)

	versionCmd.Flags().BoolVar(&shortFlag, "short", false, "display only the version number")

	return versionCmd
}

func newRunVersion(toolctlWriter io.Writer) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		return printVersion(toolctlWriter)
	}
}

func printVersion(toolctlWriter io.Writer) (err error) {

	if shortFlag {
		fmt.Fprintf(toolctlWriter, "%s\n", gitVersion)
	} else {
		var versionInfo []byte
		versionInfo, err = json.Marshal(VersionInfo{
			GitVersion:   gitVersion,
			GitCommit:    gitCommit,
			GitTreeState: gitTreeState,
			BuildDate:    buildDate,
		})
		if err != nil {
			return
		}

		fmt.Fprintf(toolctlWriter, "%s\n", string(versionInfo))
	}

	return
}
