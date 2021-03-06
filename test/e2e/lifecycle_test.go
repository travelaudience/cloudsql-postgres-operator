// +build e2e

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

package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "github.com/lib/pq"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	cloudsqladmin "google.golang.org/api/sqladmin/v1beta4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/travelaudience/cloudsql-postgres-operator/pkg/admission"
	v1alpha1api "github.com/travelaudience/cloudsql-postgres-operator/pkg/apis/cloudsql/v1alpha1"
	"github.com/travelaudience/cloudsql-postgres-operator/pkg/constants"
	"github.com/travelaudience/cloudsql-postgres-operator/test/e2e/framework"
)

const (
	// cloudsqladminUser is the name of the Cloud SQL Admin user which we lookup in the test pod's logs to understand if connection was successful.
	cloudsqladminUser = "cloudsqladmin"
	// postgresqlDriverName is the name of the SQL driver to use when connecting to PostgreSQL.
	postgresqlDriverName = "postgres"
	// postgresqlConnectionStringFormat is a format string used to build the connection string to use when connecting to PostgreSQL.
	// The formatting directives correspond to:
	// 1. Username.
	// 2. Password (URL encoded).
	// 3. Host.
	// 4. Database.
	postgresqlConnectionStringFormat = postgresqlDriverName + "://%s:%s@%s:5432/%s"
	// selectCurrentUserQuery is the SQL query that returns the name of the current user.
	selectCurrentUserQuery = "SELECT current_user;"
	// waitUntilPostgresqlInstanceStatusConditionTimeout is the timeout used while waiting for a given condition to be reported on a PostgresqlInstance resource.
	waitUntilPostgresqlInstanceStatusConditionTimeout = 10 * time.Minute
	// waitUntilPodRunningTimeout is the timeout used while waiting for a given pod to be running.
	waitUntilPodRunningTimeout = 2 * time.Minute
	// waitUntilPodLogLineMatchesTimeout is the timeout used while waiting for the logs of a given pod to match a given regular expression.
	waitUntilPodLogLineMatchesTimeout = 15 * time.Second
)

var _ = Describe("CSQLP instances", func() {
	framework.LifecycleIt("are created, updated, used and deleted as expected", func() {
		var (
			databaseInstance         *cloudsqladmin.DatabaseInstance
			db                       *sql.DB
			err                      error
			username                 string
			password                 string
			pod                      *corev1.Pod
			postgresqlInstance       *v1alpha1api.PostgresqlInstance
			postgresqlInstanceSecret *corev1.Secret
			publicIp                 string
			selectCurrentUserValue   string
		)

		By("creating a PostgresqlInstance resource with public IP disabled")

		// Create a PostgresqlInstance resource.
		var (
			availabilityType      = v1alpha1api.PostgresqlInstanceSpecAvailabilityTypeRegional
			dailyBackupsEnabled   = false
			dailyBackupsStartTime = "06:00"
			diskSizeMaximumGb     = int32(0)
			diskSizeMinimumGb     = int32(10)
			diskType              = v1alpha1api.PostgresqlInstanceSpecResourceDiskTypeHDD
			flags                 = []string{
				"log_connections=on",
				"log_disconnections=on",
			}
			instanceType = "db-custom-2-7680"
			labels       = map[string]string{
				"e2e": "true",
			}
			maintenanceDay             = v1alpha1api.PostgresqlInstanceSpecMaintenanceDayMonday
			maintenanceHour            = v1alpha1api.PostgresqlInstanceSpecMaintenanceHour("00:00")
			privateIpEnabled           = true
			privateIpNetwork           = f.BuildPrivateNetworkResourceLink(network)
			publicIpAuthorizedNetworks = v1alpha1api.PostgresqlInstanceSpecNetworkingPublicIPAuthorizedNetworkList{}
			publicIpEnabled            = false
			region                     = region
			zone                       = v1alpha1api.PostgresqlInstanceSpecLocationZoneAny
			version                    = v1alpha1api.PostgresqlInstanceSpecVersion96
		)
		postgresqlInstance = &v1alpha1api.PostgresqlInstance{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: framework.PostgresqlInstanceMetadataNamePrefix,
			},
			Spec: v1alpha1api.PostgresqlInstanceSpec{
				Availability: &v1alpha1api.PostgresqlInstanceSpecAvailability{
					Type: &availabilityType,
				},
				Backups: &v1alpha1api.PostgresqlInstanceSpecBackups{
					Daily: &v1alpha1api.PostgresqlInstancSpecBackupsDaily{
						Enabled:   &dailyBackupsEnabled,
						StartTime: &dailyBackupsStartTime,
					},
				},
				Flags:  flags,
				Labels: labels,
				Location: &v1alpha1api.PostgresqlInstanceSpecLocation{
					Region: &region,
					Zone:   &zone,
				},
				Maintenance: &v1alpha1api.PostgresqlInstanceSpecMaintenance{
					Day:  &maintenanceDay,
					Hour: &maintenanceHour,
				},
				Name: f.NewRandomPostgresqlInstanceSpecName(),
				Networking: &v1alpha1api.PostgresqlInstanceSpecNetworking{
					PrivateIP: &v1alpha1api.PostgresqlInstanceSpecNetworkingPrivateIP{
						Enabled: &privateIpEnabled,
						Network: &privateIpNetwork,
					},
					PublicIP: &v1alpha1api.PostgresqlInstanceSpecNetworkingPublicIP{
						AuthorizedNetworks: publicIpAuthorizedNetworks,
						Enabled:            &publicIpEnabled,
					},
				},
				Resources: &v1alpha1api.PostgresqlInstanceSpecResources{
					Disk: &v1alpha1api.PostgresqlInstanceSpecResourcesDisk{
						SizeMaximumGb: &diskSizeMaximumGb,
						SizeMinimumGb: &diskSizeMinimumGb,
						Type:          &diskType,
					},
					InstanceType: &instanceType,
				},
				Version: &version,
			},
		}
		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Create(postgresqlInstance)
		Expect(err).NotTo(HaveOccurred())
		Expect(postgresqlInstance).NotTo(BeNil())

		defer func() {
			By("deleting the PostgresqlInstance resource")

			err = f.DeletePostgresqlInstanceByName(postgresqlInstance.Name)
			Expect(err).NotTo(HaveOccurred())
		}()

		By(`waiting for the "Created" condition to be "True"`)

		ctx1, fn1 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn1()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx1, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeCreated, corev1.ConditionTrue)
		Expect(err).NotTo(HaveOccurred())

		By(`waiting for the "Ready" condition to be "True"`)

		ctx2, fn2 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn2()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx2, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeReady, corev1.ConditionTrue)
		Expect(err).NotTo(HaveOccurred())

		By(`checking that the CSQLP instance has all fields correctly set`)

		databaseInstance, err = f.CloudSQLClient.Instances.Get(f.ProjectId, postgresqlInstance.Spec.Name).Do()
		Expect(err).NotTo(HaveOccurred())
		Expect(databaseInstance).NotTo(BeNil())
		Expect(databaseInstance.Settings.AvailabilityType).To(Equal(postgresqlInstance.Spec.Availability.Type.APIValue()))
		Expect(databaseInstance.Settings.BackupConfiguration.Enabled).To(Equal(*postgresqlInstance.Spec.Backups.Daily.Enabled))
		Expect(databaseInstance.Settings.BackupConfiguration.StartTime).To(Equal(*postgresqlInstance.Spec.Backups.Daily.StartTime))
		Expect(databaseInstance.Settings.DatabaseFlags).To(Equal(postgresqlInstance.Spec.Flags.APIValue()))
		Expect(databaseInstance.Settings.DataDiskSizeGb).To(Equal(int64(*postgresqlInstance.Spec.Resources.Disk.SizeMinimumGb)))
		Expect(databaseInstance.Settings.DataDiskType).To(Equal(postgresqlInstance.Spec.Resources.Disk.Type.APIValue()))
		Expect(databaseInstance.Settings.IpConfiguration.Ipv4Enabled).To(Equal(*postgresqlInstance.Spec.Networking.PublicIP.Enabled))
		Expect(databaseInstance.Settings.IpConfiguration.AuthorizedNetworks).To(Equal(postgresqlInstance.Spec.Networking.PublicIP.AuthorizedNetworks.APIValue()))
		Expect(databaseInstance.Settings.IpConfiguration.PrivateNetwork).To(Equal(*postgresqlInstance.Spec.Networking.PrivateIP.Network))
		Expect(databaseInstance.Settings.MaintenanceWindow.Day).To(Equal(postgresqlInstance.Spec.Maintenance.Day.APIValue()))
		Expect(databaseInstance.Settings.MaintenanceWindow.Hour).To(Equal(postgresqlInstance.Spec.Maintenance.Hour.APIValue()))
		Expect(databaseInstance.Settings.LocationPreference.Zone).To(MatchRegexp(fmt.Sprintf("^%s-[a-z]$", region)))
		Expect(databaseInstance.Settings.Tier).To(Equal(*postgresqlInstance.Spec.Resources.InstanceType))
		Expect(databaseInstance.Settings.StorageAutoResizeLimit).To(Equal(int64(*postgresqlInstance.Spec.Resources.Disk.SizeMaximumGb)))
		Expect(*databaseInstance.Settings.StorageAutoResize).To(Equal(*postgresqlInstance.Spec.Resources.Disk.SizeMaximumGb == int32(0)))
		Expect(databaseInstance.Settings.UserLabels).To(Equal(postgresqlInstance.Spec.Labels))

		By(`checking that the private IP of the CSQLP instance has been reported (and that no public IP has)`)

		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Get(postgresqlInstance.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(postgresqlInstance.Status.IPs.PrivateIP).NotTo(BeEmpty())
		Expect(postgresqlInstance.Status.IPs.PublicIP).To(BeEmpty())

		By(`checking that the secret containing the password for the "postgres" user has been created and contains the PGUSER and PGPASS keys`)

		postgresqlInstanceSecret, err = f.KubeClient.CoreV1().Secrets(f.Namespace).Get(postgresqlInstance.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(postgresqlInstanceSecret.Data).To(HaveKey(constants.PostgresqlInstanceUsernameKey))
		Expect(postgresqlInstanceSecret.Data).To(HaveKey(constants.PostgresqlInstancePasswordKey))

		By(`checking that the PGUSER and PGPASS keys have adequate values`)

		username = string(postgresqlInstanceSecret.Data[constants.PostgresqlInstanceUsernameKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(username).To(Equal(constants.PostgresqlInstanceUsernameValue))
		password = string(postgresqlInstanceSecret.Data[constants.PostgresqlInstancePasswordKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(password).NotTo(BeNil())

		By(`enabling public IP networking and adding the external IP of the current host to the list of authorized networks`)

		publicIpEnabled = true
		publicIpAuthorizedNetworks = append(publicIpAuthorizedNetworks, v1alpha1api.PostgresqlInstanceSpecNetworkingPublicIPAuthorizedNetwork{
			Cidr: fmt.Sprintf("%s/32", f.ExternalIP),
		})
		postgresqlInstance.Spec.Networking.PublicIP.Enabled = &publicIpEnabled
		postgresqlInstance.Spec.Networking.PublicIP.AuthorizedNetworks = publicIpAuthorizedNetworks
		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Update(postgresqlInstance)
		Expect(err).NotTo(HaveOccurred())

		By(`waiting for the "Ready" condition to switch to "False" and back to "True"`)

		ctx3, fn3 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn3()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx3, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeReady, corev1.ConditionFalse)
		Expect(err).NotTo(HaveOccurred())

		ctx4, fn4 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn4()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx4, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeReady, corev1.ConditionTrue)
		Expect(err).NotTo(HaveOccurred())

		By(`checking that a public IP for the CSQLP instance has now been reported, and that the private IP keeps being reported`)

		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Get(postgresqlInstance.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		publicIp = postgresqlInstance.Status.IPs.PublicIP
		Expect(publicIp).NotTo(BeEmpty())
		Expect(postgresqlInstance.Status.IPs.PrivateIP).NotTo(BeEmpty())

		By(`attempting to connect to the instance via its public IP using the values of PGUSER and PGPASS`)

		db, err = sql.Open(postgresqlDriverName, fmt.Sprintf(postgresqlConnectionStringFormat, username, url.QueryEscape(password), publicIp, username))
		Expect(err).NotTo(HaveOccurred())
		err = db.Ping()
		Expect(err).NotTo(HaveOccurred())

		err = db.QueryRow(selectCurrentUserQuery).Scan(&selectCurrentUserValue)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectCurrentUserValue).To(Equal(username))

		err = db.Close()
		Expect(err).NotTo(HaveOccurred())

		By(`launching a test pod requesting access to the CSQLP instance, verifying it has been injected with the Cloud SQL proxy sidecar container, and waiting for it to be ready`)

		// Launch the test pod, asking it to list users (i.e. "\du;") using "psql" after it starts running.
		pod, err = f.CreatePostgresqlTestPod(postgresqlInstance, `\du;`)
		Expect(err).NotTo(HaveOccurred())

		// Make sure that the Cloud SQL proxy sidecar container has been injected.
		Expect(pod.Annotations).To(HaveKeyWithValue(constants.ProxyInjectedAnnotationKey, "true"))
		containerNames := make([]string, len(pod.Spec.Containers))
		for _, container := range pod.Spec.Containers {
			containerNames = append(containerNames, container.Name)
		}
		Expect(containerNames).To(ContainElement(admission.CloudSQLProxyContainerName))

		// Make sure that the "PG*" environment variables have been injected in each container besides the Cloud SQL proxy one.
		for _, container := range pod.Spec.Containers {
			if container.Name != admission.CloudSQLProxyContainerName {
				envvarNames := make([]string, len(container.Env))
				for _, envvar := range container.Env {
					envvarNames = append(envvarNames, envvar.Name)
				}
				Expect(envvarNames).To(ContainElement(admission.PghostEnvVarName))
				Expect(envvarNames).To(ContainElement(admission.PgportEnvVarName))
				Expect(envvarNames).To(ContainElement(admission.PguserEnvVarName))
				Expect(envvarNames).To(ContainElement(admission.PgpassfileEnvVarName))
			}
		}

		// Wait until the test pod is running before proceeding.
		ctx5, fn5 := context.WithTimeout(context.Background(), waitUntilPodRunningTimeout)
		defer fn5()
		err = f.WaitUntilPodRunning(ctx5, pod)
		Expect(err).NotTo(HaveOccurred())

		By(`checking the logs of the test pod and making sure connection was successful, and that the "cloudsqladmin" user is listed`)

		ctx6, fn6 := context.WithTimeout(context.Background(), waitUntilPodLogLineMatchesTimeout)
		defer fn6()
		err = f.WaitUntilPodLogLineMatches(ctx6, pod, cloudsqladminUser)
		Expect(err).NotTo(HaveOccurred())

		// If we've been told not to test private IP access to the CSQLP instance, we may return now.
		if !testPrivateIpAccess {
			return
		}

		By(`disabling public IP networking and removing the external IP of the current host from the list of authorized networks`)

		publicIpEnabled = false
		publicIpAuthorizedNetworks = make([]v1alpha1api.PostgresqlInstanceSpecNetworkingPublicIPAuthorizedNetwork, 0)
		postgresqlInstance.Spec.Networking.PublicIP.Enabled = &publicIpEnabled
		postgresqlInstance.Spec.Networking.PublicIP.AuthorizedNetworks = publicIpAuthorizedNetworks
		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Update(postgresqlInstance)
		Expect(err).NotTo(HaveOccurred())

		By(`waiting for the "Ready" condition to switch to "False" and back to "True"`)

		ctx7, fn7 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn7()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx7, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeReady, corev1.ConditionFalse)
		Expect(err).NotTo(HaveOccurred())

		ctx8, fn8 := context.WithTimeout(context.Background(), waitUntilPostgresqlInstanceStatusConditionTimeout)
		defer fn8()
		err = f.WaitUntilPostgresqlInstanceStatusCondition(ctx8, postgresqlInstance, v1alpha1api.PostgresqlInstanceStatusConditionTypeReady, corev1.ConditionTrue)
		Expect(err).NotTo(HaveOccurred())

		By(`checking that the private IP for the CSQLP instance keeps being reported, and that the public IP is no longer reported`)

		postgresqlInstance, err = f.SelfClient.CloudsqlV1alpha1().PostgresqlInstances().Get(postgresqlInstance.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(postgresqlInstance.Status.IPs.PublicIP).To(BeEmpty())
		Expect(postgresqlInstance.Status.IPs.PrivateIP).NotTo(BeEmpty())

		By(`launching a new test pod that will connect to the CSQLP instance using its private IP`)

		// Launch the test pod, asking it to list users (i.e. "\du;") using "psql" after it starts running.
		pod, err = f.CreatePostgresqlTestPod(postgresqlInstance, `\du;`)
		Expect(err).NotTo(HaveOccurred())

		// Wait until the test pod is running before proceeding.
		ctx9, fn9 := context.WithTimeout(context.Background(), waitUntilPodRunningTimeout)
		defer fn9()
		err = f.WaitUntilPodRunning(ctx9, pod)
		Expect(err).NotTo(HaveOccurred())

		By(`checking the logs of the test pod and making sure connection was successful, and that the "cloudsqladmin" user is listed`)

		ctx10, fn10 := context.WithTimeout(context.Background(), waitUntilPodLogLineMatchesTimeout)
		defer fn10()
		err = f.WaitUntilPodLogLineMatches(ctx10, pod, cloudsqladminUser)
		Expect(err).NotTo(HaveOccurred())
	})
})
