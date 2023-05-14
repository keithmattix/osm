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
	var sidecarTimeout int64 = 15
	var ns = []string{sourceName, destName}

	It("Tests HTTP idle timeout by ensuring requests that take less than the timeout succeed", func() {
		// Install OSM
		Expect(Td.InstallOSM(Td.GetOSMInstallOpts())).To(Succeed())
		meshConfig, _ := Td.GetMeshConfig(Td.OsmNamespace)
		meshConfig.Spec.Sidecar.HTTPIdleTimeout = sidecarTimeout

		_, err := Td.UpdateOSMConfig(meshConfig)

		Expect(err).NotTo(HaveOccurred())

		// Create Test NS
		for _, n := range ns {
			Expect(Td.CreateNs(n, nil)).To(Succeed())
		}
		// Only add source namespace to the mesh, destination is simulating an external cluster
		Expect(Td.AddNsToMesh(true, sourceName)).To(Succeed())

		// Set up the destination HTTP server. It is not part of the mesh
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				PodName:   destName,
				Namespace: destName,
				Image:     fortioImageName,
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
		// TODO: Add delay via query param
		clientToServer := HTTPRequestDef{
			SourceNs:        sourceName,
			SourcePod:       srcPod.Name,
			SourceContainer: srcPod.Name,
			Destination:     fmt.Sprintf("%s.%s:%d?delay=%ds", dstSvc.Name, dstSvc.Namespace, fortioHTTPPort, sidecarTimeout-1), // Make sure requests less than the timeout succeed
		}

		srcToDestStr := fmt.Sprintf("%s -> %s",
			fmt.Sprintf("%s/%s", sourceName, srcPod.Name),
			clientToServer.Destination)

		cond := Td.WaitForRepeatedSuccess(func() bool {
			result := Td.HTTPRequest(clientToServer)

			if result.Err != nil || result.StatusCode != 200 {
				Td.T.Logf("> (%s) HTTP Req failed %d %v",
					srcToDestStr, result.StatusCode, result.Err)
				return false
			}
			Td.T.Logf("> (%s) HTTP Req succeeded: %d", srcToDestStr, result.StatusCode)
			return true
		}, 3, 120*time.Second)

		Expect(cond).To(BeTrue(), "Failed testing HTTP idleTimeout of %ds with a fortio delay of %ds", sidecarTimeout, sidecarTimeout-1)
	})

	It("Tests HTTP idle timeout by ensuring requests that take more than the timeout fail", func() {
		// Install OSM
		Expect(Td.InstallOSM(Td.GetOSMInstallOpts())).To(Succeed())
		meshConfig, _ := Td.GetMeshConfig(Td.OsmNamespace)
		meshConfig.Spec.Sidecar.HTTPIdleTimeout = sidecarTimeout

		_, err := Td.UpdateOSMConfig(meshConfig)

		Expect(err).NotTo(HaveOccurred())

		// Create Test NS
		for _, n := range ns {
			Expect(Td.CreateNs(n, nil)).To(Succeed())
		}
		// Only add source namespace to the mesh, destination is simulating an external cluster
		Expect(Td.AddNsToMesh(true, sourceName)).To(Succeed())

		// Set up the destination HTTP server. It is not part of the mesh
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				PodName:   destName,
				Namespace: destName,
				Image:     fortioImageName,
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
		clientToServer := HTTPRequestDef{
			SourceNs:        sourceName,
			SourcePod:       srcPod.Name,
			SourceContainer: srcPod.Name,
			Destination:     fmt.Sprintf("%s.%s:%d?delay=%ds", dstSvc.Name, dstSvc.Namespace, fortioHTTPPort, sidecarTimeout+1), // Make sure requests greater than the timeout fail
		}

		srcToDestStr := fmt.Sprintf("%s -> %s",
			fmt.Sprintf("%s/%s", sourceName, srcPod.Name),
			clientToServer.Destination)

		cond := Td.WaitForRepeatedSuccess(func() bool {
			result := Td.HTTPRequest(clientToServer)

			if result.Err != nil {
				Td.T.Logf("> (%s) HTTP Req had an error %d %v",
					srcToDestStr, result.StatusCode, result.Err)
				return false
			}

			if result.StatusCode != 503 {
				Td.T.Logf("> (%s) HTTP Req did not return 503 as expected (due to exeecding the timeout). Instead received %d %v",
					srcToDestStr, result.StatusCode, result.Err)
				return false
			}
			Td.T.Logf("> (%s) HTTP Req failed due to timeout as expected: %d", srcToDestStr, result.StatusCode)
			return true
		}, 3, 120*time.Second)

		Expect(cond).To(BeTrue(), "Failed testing HTTP idleTimeout of %ds with a fortio delay of %ds", sidecarTimeout, sidecarTimeout-1)
	})
}
