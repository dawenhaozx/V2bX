package cmd

import (
	_ "github.com/InazumaV/V2bX/core/imports"
	"github.com/spf13/cobra"
)

var command = &cobra.Command{
	Use: "V2bX",
}

func Run() error {
	return command.Execute()
}
