package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/openservicemesh/osm/tests/framework"
)

var _ = OSMDescribe("Test idleTimeout",
	OSMDescribeInfo{
		Tier:   1,
		Bucket: 5,
		OS:     OSCrossPlatform,
	},
	func() {
		Context("Test HTTP idle timeout", func() {
			testHTTPIdleTimeout()
		})
	})

func testHTTPIdleTimeout() {
	const sourceName = "client"
	const destName = "server"
	var sidecarTimeout int64 = 60 * 6 // 6 minutes since the default idle timeout is 5 minutes
	var ns = []string{sourceName, destName}

	It("Tests HTTP idle timeout by ensuring requests that take less than the timeout succeed", func() {
		// Install OSM
		Expect(Td.InstallOSM(Td.GetOSMInstallOpts())).To(Succeed())
		meshConfig, _ := Td.GetMeshConfig(Td.OsmNamespace)
		meshConfig.Spec.Sidecar.HTTPIdleTimeout = sidecarTimeout
		meshConfig.Spec.Traffic.EnablePermissiveTrafficPolicyMode = true
		meshConfig.Spec.Traffic.EnableEgress = true

		_, err := Td.UpdateOSMConfig(meshConfig)

		Expect(err).NotTo(HaveOccurred())

		// Create Test NS
		for _, n := range ns {
			Expect(Td.CreateNs(n, nil)).To(Succeed())
			Expect(Td.AddNsToMesh(true, n)).To(Succeed())
		}

		// Set up the destination HTTP server. It is not part of the mesh
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				PodName:   destName,
				Namespace: destName,
				Image:     fortioImageName,
				Command:   []string{"/usr/bin/fortio", "server", "-config-dir", "/etc/fortio", "--max-echo-delay", "24h"},
				Ports:     []int{fortioHTTPPort},
				OS:        Td.ClusterOS,
			})
		Expect(err).NotTo(HaveOccurred())

		_, err = Td.CreateServiceAccount(destName, &svcAccDef)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreatePod(destName, podDef)
		Expect(err).NotTo(HaveOccurred())
		dstSvc, err := Td.CreateService(destName, svcDef)
		Expect(err).NotTo(HaveOccurred())

		// Expect it to be up and running in it's receiver namespace
		Expect(Td.WaitForPodsRunningReady(destName, 90*time.Second, 1, nil)).To(Succeed())

		srcPod := setupSource(sourceName, true)
		fortioTimeout := sidecarTimeout - 1
		// All ready. Expect client to reach server
		clientToServer := HTTPRequestDef{
			SourceNs:        sourceName,
			SourcePod:       srcPod.Name,
			SourceContainer: srcPod.Name,
			Destination:     fmt.Sprintf("%s.%s:%d?delay=%ds", dstSvc.Name, dstSvc.Namespace, fortioHTTPPort, fortioTimeout), // Make sure requests less than the timeout succeed
		}

		srcToDestStr := fmt.Sprintf("%s -> %s",
			fmt.Sprintf("%s/%s", sourceName, srcPod.Name),
			clientToServer.Destination)

		result := Td.HTTPRequest(clientToServer)

		if result.Err != nil {
			Td.T.Logf("> (%s) HTTP Req failed with err %v", srcToDestStr, result.Err)
		}

		Expect(result.StatusCode).To(Equal(200), "Failed testing HTTP idleTimeout of %ds with a fortio delay of %ds. Expected 200 received %d or err %v. Body was: %s", sidecarTimeout, fortioTimeout, result.StatusCode, result.Err, result.Body)
	})

	It("Tests HTTP idle timeout by ensuring requests that take more than the timeout fail", func() {
		// Install OSM
		Expect(Td.InstallOSM(Td.GetOSMInstallOpts())).To(Succeed())
		meshConfig, _ := Td.GetMeshConfig(Td.OsmNamespace)
		meshConfig.Spec.Sidecar.HTTPIdleTimeout = sidecarTimeout
		meshConfig.Spec.Traffic.EnablePermissiveTrafficPolicyMode = true
		meshConfig.Spec.Traffic.EnableEgress = true

		_, err := Td.UpdateOSMConfig(meshConfig)

		Expect(err).NotTo(HaveOccurred())

		// Create Test NS
		for _, n := range ns {
			Expect(Td.CreateNs(n, nil)).To(Succeed())
			Expect(Td.AddNsToMesh(true, n)).To(Succeed())
		}

		// Set up the destination HTTP server. It is not part of the mesh
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				PodName:   destName,
				Namespace: destName,
				Image:     fortioImageName,
				Command:   []string{"/usr/bin/fortio", "server", "-config-dir", "/etc/fortio", "--max-echo-delay", "24h"},
				Ports:     []int{fortioHTTPPort},
				OS:        Td.ClusterOS,
			})
		Expect(err).NotTo(HaveOccurred())

		_, err = Td.CreateServiceAccount(destName, &svcAccDef)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreatePod(destName, podDef)
		Expect(err).NotTo(HaveOccurred())
		dstSvc, err := Td.CreateService(destName, svcDef)
		Expect(err).NotTo(HaveOccurred())

		// Expect it to be up and running in it's receiver namespace
		Expect(Td.WaitForPodsRunningReady(destName, 90*time.Second, 1, nil)).To(Succeed())

		srcPod := setupSource(sourceName, false)
		// All ready. Expect client to reach server
		fortioDelay := sidecarTimeout + 5
		clientToServer := HTTPRequestDef{
			SourceNs:        sourceName,
			SourcePod:       srcPod.Name,
			SourceContainer: srcPod.Name,
			Destination:     fmt.Sprintf("%s.%s:%d?delay=%ds", dstSvc.Name, dstSvc.Namespace, fortioHTTPPort, fortioDelay), // Make sure requests greater than the timeout fail
		}

		srcToDestStr := fmt.Sprintf("%s -> %s",
			fmt.Sprintf("%s/%s", sourceName, srcPod.Name),
			clientToServer.Destination)

		result := Td.HTTPRequest(clientToServer)

		if result.Err != nil {
			Td.T.Logf("> (%s) HTTP Req had an error %d %v",
				srcToDestStr, result.StatusCode, result.Err)
		}

		Expect(result.StatusCode).To(Equal(504), "Failed testing HTTP idleTimeout of %ds with a fortio delay of %ds. Expected 504 received %d or err %v. Body was %s", sidecarTimeout, fortioDelay, result.StatusCode, result.Err, result.Body)
	})
}
