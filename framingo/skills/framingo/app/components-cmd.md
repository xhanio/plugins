# Cmd Component — Cobra CLI Wiring

`pkg/components/cmd/` holds one package per binary persona. The example
ships two — `cmd/app` behind the `exampleapp` binary and `cmd/cli` behind
`examplecli` — and the mains under `build/binary/<name>/main.go` are
deliberately thin:

```go
// build/binary/exampleapp/main.go — the whole file
func main() {
	rootCmd := app.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

Everything real lives in the component packages; the `main` never grows.

## `cmd/app` — The Daemon Binary

Three files: `root.go` (root command + global flags), `daemon.go` (the
subcommand that runs the [server component](components-server.md)), and
`common.go` (the `version` subcommand printing `info.GetBuildInfo()`).

```go
// components/cmd/app/root.go
var (
	help    bool
	verbose bool
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if help {
				_ = cmd.Help()
				os.Exit(0)
			}
		},
	}
	root.PersistentFlags().BoolVar(&help, "help", false, "")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "")

	root.AddCommand(NewDaemonCmd())
	root.AddCommand(NewVersionCmd())
	return root
}
```

```go
// components/cmd/app/daemon.go
var configPath string

func NewDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "daemon",
		RunE:         runDaemon,
		SilenceUsage: true,
	}
	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.json", "config file path")
	return cmd
}

func runDaemon(cmd *cobra.Command, args []string) error {
	m := example.New(configPath) // the server component
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		return errors.Wrap(err)
	}
	if err := m.Start(ctx); err != nil { // blocks in the signal loop until shutdown
		return errors.Wrap(err)
	}
	return nil
}
```

## `cmd/cli` — The Operator CLI

The CLI consumes the [client component](components-client.md), never
`client.Client` directly. The SDK is built once in the root command's
`PersistentPreRunE`; one file per API domain then adds subcommands that call
its methods. The example's files: `auth.go` (login/logout), `example.go`
(helloworld), `cert.go` (certificate generation), `messagebus.go` (message
stream), `common.go` (version).

```go
// components/cmd/cli/root.go
var (
	help     bool
	verbose  bool
	endpoint string
	credFile string

	cli example.Client // the client component, shared by all subcommands
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if help {
				_ = cmd.Help()
				os.Exit(0)
			}
			cu, err := user.Current()
			if err != nil {
				return errors.Wrap(err)
			}
			credFile = filepath.Join(cu.HomeDir, ".example") // session survives between runs
			opts := []example.Option{
				example.WithCredential(credFile),
				example.WithEndpoint(endpoint),
			}
			if verbose {
				opts = append(opts, example.WithDebug())
			}
			cli = example.New(opts...)
			if err := cli.Init(); err != nil {
				return errors.Wrap(err)
			}
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&help, "help", false, "")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "")
	root.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", "", "server endpoint")

	root.AddCommand(NewVersionCmd())
	root.AddCommand(NewLoginCmd())
	root.AddCommand(NewLogoutCmd())
	root.AddCommand(NewHelloworldCmd())
	root.AddCommand(NewCertGenCmd())
	root.AddCommand(NewMessageStreamCmd())
	return root
}
```

The `--endpoint` default is deliberately empty — the client component falls
back to its own `DefaultEndpoint` in `Init`.

A subcommand file stays declarative: parse args, call one SDK method.

```go
// components/cmd/cli/example.go
func runHelloworld(cmd *cobra.Command, args []string) error {
	var message string
	if len(args) > 0 {
		message = args[0]
	}
	if err := cli.HelloWorld(context.Background(), message); err != nil {
		return errors.Wrap(err)
	}
	return nil
}
```

Auth state (the credential file, the session header) is the client
component's job, not the CLI's.
