package cmd_test

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/toolctl/toolctl/internal/cmd"
)

func TestAPICmd(t *testing.T) {
	tests := []test{
		{
			name:    "no cli args",
			cliArgs: []string{},
			wantOut: `Commands for managing the toolctl API

Usage:
  toolctl api [command]

Available Commands:
  discover    Discover new versions of one or more tools

Flags:
  -h, --help   help for api

Global Flags:
      --config string   path of the config file (default is $HOME/.config/toolctl/config.yaml)

Use "toolctl api [command] --help" for more information about a command.
`,
		},
	}

	for _, tt := range tests {
		localAPIFS, downloadServer, err := setupLocalAPI()
		if err != nil {
			t.Fatal(err)
		}

		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)

			command := cmd.NewRootCmd(buf, localAPIFS)
			command.SetArgs(append([]string{"api"}, tt.cliArgs...))

			// Redirect Cobra output
			command.SetOut(buf)
			command.SetErr(buf)

			err := command.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantOut == "" {
				t.Fatalf("wantOut must be set")
			}
			if diff := cmp.Diff(tt.wantOut, buf.String()); diff != "" {
				t.Errorf("Output mismatch (-want +got):\n%s", diff)
			}
		})

		downloadServer.Close()
	}
}
