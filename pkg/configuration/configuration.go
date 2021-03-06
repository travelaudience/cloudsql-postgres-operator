/*
Copyright 2019 The cloudsql-postgres-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configuration

import (
	"io/ioutil"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"

	"github.com/travelaudience/cloudsql-postgres-operator/pkg/constants"
)

const (
	// defaultAdminServiceAccountKeyPath is the default value of "gcp.admin_service_account_key_path".
	defaultAdminServiceAccountKeyPath = "/secret/admin-key.json"
	// defaultClientServiceAccountKeyPath is the default value of "gcp.client_service_account_key_path".
	defaultClientServiceAccountKeyPath = "/secret/client-key.json"
)

// Admission holds admission-related configuration options.
type Admission struct {
	// BindAddress is the "host:port" pair where the admission webhook is to be served.
	BindAddress string `toml:"bind_address"`
	// CloudSQLProxyImage is the image to use when injecting the Cloud SQL proxy in pods requesting access to a CSQLP instance.
	CloudSQLProxyImage string `toml:"cloud_sql_proxy_image"`
}

// setDefaults sets default values where necessary.
func (a *Admission) setDefaults() {
	if a.BindAddress == "" {
		a.BindAddress = constants.DefaultWebhookBindAddress
	}
	if a.CloudSQLProxyImage == "" {
		a.CloudSQLProxyImage = constants.DefaultCloudSQLProxyImage
	}
}

// Cluster holds cluster-related configuration options.
type Cluster struct {
	// Kubeconfig holds the path to the kubeconfig file to use (may be empty for in-cluster configuration).
	Kubeconfig string `toml:"kubeconfig"`
	// Namespace holds the namespace where cloudsql-postgres-operator is deployed.
	Namespace string `toml:"namespace"`
}

// setDefaults sets default values where necessary.
func (c *Cluster) setDefaults() {
	if c.Namespace == "" {
		c.Namespace = constants.DefaultCloudsqlPostgresOperatorNamespace
	}
}

// Configuration is the root configuration object.
type Configuration struct {
	// Admission holds admission-related configuration options.
	Admission Admission `toml:"admission"`
	// Cluster holds cluster-related configuration options.
	Cluster Cluster `toml:"cluster"`
	// Controllers holds controller-related configuration options.
	Controllers Controllers `toml:"controllers"`
	// GCP holds GCP-related configuration options.
	GCP GCP `toml:"gcp"`
	// Logging holds logging-related configuration options.
	Logging Logging `toml:"logging"`
}

// SetDefaults sets default values where necessary.
func (c *Configuration) setDefaults() {
	c.Admission.setDefaults()
	c.Cluster.setDefaults()
	c.Controllers.setDefaults()
	c.GCP.setDefaults()
	c.Logging.setDefaults()
}

// Controllers holds controller-related configuration options.
type Controllers struct {
	// ResyncPeriodSeconds holds the resync period to use for the controllers, expressed in seconds.
	ResyncPeriodSeconds int32 `toml:"resync_period_seconds"`
}

// setDefaults sets default values where necessary.
func (c *Controllers) setDefaults() {
	if c.ResyncPeriodSeconds == 0 {
		c.ResyncPeriodSeconds = constants.DefaultControllersResyncPeriodSeconds
	}
}

// Logging holds logging-related configuration options.
type Logging struct {
	// Level holds the log level to use (possible values: "trace", "debug", "info", "warn", "error", "fatal" and "panic").
	Level string `toml:"level"`
}

// setDefaults sets default values where necessary.
func (l *Logging) setDefaults() {
	if l.Level == "" {
		l.Level = log.InfoLevel.String()
	}
}

// GCP holds project-related configuration options.
type GCP struct {
	// AdminServiceAccountKeyPath holds the path to the file that contains credentials for an IAM service account with the "roles/cloudsql.admin" role.
	AdminServiceAccountKeyPath string `toml:"admin_service_account_key_path"`
	// ClientServiceAccountKeyPath holds the path to the file that contains credentials for an IAM service account with the "roles/cloudsql.client" role.
	ClientServiceAccountKeyPath string `toml:"client_service_account_key_path"`
	// ProjectID holds the ID of the Google Cloud Platform project where cloudsql-postgres-operator is managing Cloud SQL instances.
	ProjectID string `toml:"project_id"`
}

// setDefaults sets default values where necessary.
func (g *GCP) setDefaults() {
	if g.AdminServiceAccountKeyPath == "" {
		g.AdminServiceAccountKeyPath = defaultAdminServiceAccountKeyPath
	}
	if g.ClientServiceAccountKeyPath == "" {
		g.ClientServiceAccountKeyPath = defaultClientServiceAccountKeyPath
	}
}

// MustNewConfigurationFromFile attempts to parse the specified configuration file, exiting the application if it cannot be parsed.
func MustNewConfigurationFromFile(path string) Configuration {
	if path == "" {
		log.Fatalf("the path to the configuration file must not be empty")
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read the configuration file: %v", err)
	}
	var r Configuration
	if err := toml.Unmarshal(b, &r); err != nil {
		log.Fatalf("failed to read the configuration file: %v", err)
	}
	r.setDefaults()
	return r
}
