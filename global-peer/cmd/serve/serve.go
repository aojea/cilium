// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package serve

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cilium/cilium/pkg/crypto/certloader"
	globalpeerdefaults "github.com/cilium/cilium/pkg/hubble/global-peer/defaults"
	"github.com/cilium/cilium/pkg/hubble/global-peer/server"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

const (
	keyDebug             = "debug"
	keyConfigPath        = "config"
	keyClusterName       = "cluster-name"
	keyListenAddress     = "org-peer-listen-address"
	keyDialTimeout       = "dial-timeout"
	keyRetryTimeout      = "retry-timeout"
	keyLocalPeerService  = "local-peer-service"
	keyTLSClientCertFile = "tls-client-cert-file"
	keyTLSClientKeyFile  = "tls-client-key-file"
	keyTLSClientCAFiles  = "tls-client-ca-files"
	keyTLSServerCertFile = "tls-server-cert-file"
	keyTLSServerKeyFile  = "tls-server-key-file"
	keyTLSServerCAFiles  = "tls-server-ca-files"
	keyTLSDisabled       = "disable-tls"
)

// New creates a new serve command.
func New(vp *viper.Viper) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the gRPC server",
		Long:  `Run the gRPC server with Hubble Peer interface.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServe(context.Background(), vp)
		},
	}
	flags := cmd.Flags()
	flags.Bool(
		keyDebug,
		false,
		"Enable debug messages",
	)
	flags.String(
		keyConfigPath,
		globalpeerdefaults.ConfigPath,
		"Path to config file",
	)
	flags.String(
		keyClusterName,
		globalpeerdefaults.ClusterName,
		"Name of the current cluster",
	)
	flags.String(
		keyListenAddress,
		globalpeerdefaults.ListenAddress,
		"Address on which to listen",
	)
	flags.Duration(
		keyDialTimeout,
		globalpeerdefaults.DialTimeout,
		"Dial timeout when connecting to hubble peers",
	)
	flags.String(
		keyLocalPeerService,
		globalpeerdefaults.LocalPeerTarget,
		"Address of the server that implements the peer gRPC service",
	)
	flags.Duration(
		keyRetryTimeout,
		globalpeerdefaults.RetryTimeout,
		"Time to wait before attempting to reconnect to a hubble peer when the connection is lost",
	)
	flags.String(
		keyTLSClientCertFile,
		"",
		"Path to the public key file for the client certificate to connect to Hubble server instances. The file must contain PEM encoded data.",
	)
	flags.String(
		keyTLSClientKeyFile,
		"",
		"Path to the private key file for the client certificate to connect to Hubble server instances. The file must contain PEM encoded data.",
	)
	flags.StringSlice(
		keyTLSClientCAFiles,
		[]string{},
		"Paths to one or more public key files of the CA which to use to verify Hubble server.",
	)
	flags.String(
		keyTLSServerCertFile,
		"",
		"Path to the public key file for the Hubble Relay server. The file must contain PEM encoded data.",
	)
	flags.String(
		keyTLSServerKeyFile,
		"",
		"Path to the private key file for the Hubble Relay server. The file must contain PEM encoded data.",
	)
	flags.StringSlice(
		keyTLSServerCAFiles,
		[]string{},
		"Paths to one or more public key files of the CA which to use to verify Global-peer's clients.",
	)
	flags.Bool(
		keyTLSDisabled,
		false,
		"Disable mTLS and allow the connection to be over plaintext.",
	)
	vp.BindPFlags(flags)

	return cmd
}

func runServe(ctx context.Context, vp *viper.Viper) error {
	if vp.GetBool(keyDebug) {
		logging.SetLogLevelToDebug()
	}
	logger := logging.DefaultLogger.WithField(logfields.LogSubsys, "global-peer")
	if configPath := vp.GetString(keyConfigPath); configPath != "" {
		vp.SetConfigFile(configPath)
		if err := vp.MergeInConfig(); err != nil {
			return fmt.Errorf("parse %q config: %v", configPath, err)
		}
	}

	opts := server.Options{
		ClusterName:   vp.GetString(keyClusterName),
		ListenAddress: vp.GetString(keyListenAddress),
		DialTimeout:   vp.GetDuration(keyDialTimeout),
		RetryTimeout:  vp.GetDuration(keyRetryTimeout),
		PeerTarget:    vp.GetString(keyLocalPeerService),
	}

	if vp.GetBool(keyTLSDisabled) {
		opts.NoTLS = true
	} else {
		var err error
		opts.ClientTLSConfig, err = certloader.NewWatchedClientConfig(
			logger.WithField("config", "global-peer-to-anetd"),
			vp.GetStringSlice(keyTLSClientCAFiles),
			vp.GetString(keyTLSClientCertFile),
			vp.GetString(keyTLSClientKeyFile),
		)
		if err != nil {
			return fmt.Errorf("TLS client configuration setup: %v", err)
		}
		opts.ServerTLSConfig, err = certloader.NewWatchedServerConfig(
			logger.WithField("config", "global-peer-to-hubble-relay"),
			vp.GetStringSlice(keyTLSServerCAFiles),
			vp.GetString(keyTLSServerCertFile),
			vp.GetString(keyTLSServerKeyFile),
		)
		if err != nil {
			return fmt.Errorf("TLS server configuration setup: %v", err)
		}
	}

	srv, err := server.New(logger, opts)
	if err != nil {
		return fmt.Errorf("cannot create global-peer server: %v", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		select {
		case s := <-sigs:
			logger.Infof("Closing server due to signal %v", s)
			cancel()
		case <-ctx.Done():
			logger.Debug("Closing server due to context")
		}
	}()
	return srv.Run(ctx)
}
