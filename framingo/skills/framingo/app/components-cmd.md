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

Four files: `root.go` (root command + global flags), `daemon.go` (the
subcommand that runs the [server component](components-server.md)),
`migrate.go` (the subcommand that applies the schema and exits), and
`common.go` (the `version` subcommand printing `info.GetBuildInfo()` as JSON).

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
	root.AddCommand(NewMigrateCmd())
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
	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "config file path")
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

`migrate` is its own step so a deployment migrates once, not on every
replica start — the prod Job runs `exampleapp migrate -c …`. It calls the
server component's `Migrate`, which initializes the database alone and
returns (see [components-server.md](components-server.md#migratego--the-one-off-schema-run)):

```go
// components/cmd/app/migrate.go
func NewMigrateCmd() *cobra.Command {
	var migrateConfig string
	cmd := &cobra.Command{
		Use:          "migrate",
		Short:        "Apply the database migrations db.migration names, then exit",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.Wrap(example.Migrate(context.Background(), migrateConfig))
		},
	}
	cmd.Flags().StringVarP(&migrateConfig, "config", "c", "config.yaml", "config file path")
	return cmd
}
```

## `cmd/cli` — The Operator CLI

The CLI consumes the [client component](components-client.md), never
`client.Client` directly. The SDK is built once in the root command's
`PersistentPreRunE`; one file per API domain then adds subcommands that call
its methods. The example's files and the subcommands they add, all at the
top level: `auth.go` (`login [-u user]`, which prompts for the password,
and `logout`), `example.go` (`helloworld <message>`), `cert.go` (`certutil`,
generating a CA and a server cert locally), `messagebus.go` (`stream`, the
message stream), `common.go` (`version`).

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
back to its own `DefaultEndpoint` (`http://127.0.0.1:8080/api/v1`) in
`Init`. An endpoint passed in is the API base, prefix included: request
paths join onto it as they are.

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
