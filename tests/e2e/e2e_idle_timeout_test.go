package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	policyv1alpha1 "github.com/openservicemesh/osm/pkg/apis/policy/v1alpha1"

	. "github.com/openservicemesh/osm/tests/framework"
)

var _ = OSMDescribe("Test idleTimeout",
	OSMDescribeInfo{
		Tier:   1,
		Bucket: 12,
		OS:     OSCrossPlatform,
	},
	func() {
		Context("Test HTTP idle timeout", func() {
			testHTTPIdleTimeout()
		})
		Context("Test ingress HTTP idle timeout", func() {
			testIngressHTTPIdleTimeout()
		})
	},
)

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

func testIngressHTTPIdleTimeout() {
	var (
		destName             = "server"
		destNs               = RandomNameWithPrefix(destName)
		sidecarTimeout int64 = 60 * 6 // 6 minutes since the default idle timeout is 5 minutes
	)

	It("Tests Ingress HTTP idle timeout by ensuring requests that take less than the timeout succeed", func() {
		// Install OSM
		installOpts := Td.GetOSMInstallOpts()
		Expect(Td.InstallOSM(installOpts)).To(Succeed())
		meshConfig, _ := Td.GetMeshConfig(Td.OsmNamespace)
		meshConfig.Spec.Sidecar.HTTPIdleTimeout = sidecarTimeout
		meshConfig.Spec.Traffic.EnablePermissiveTrafficPolicyMode = true
		meshConfig.Spec.Traffic.EnableEgress = true

		_, err := Td.UpdateOSMConfig(meshConfig)

		Expect(err).NotTo(HaveOccurred())

		Expect(Td.CreateNs(destNs, nil)).To(Succeed())
		Expect(Td.AddNsToMesh(true, destNs)).To(Succeed())

		// Get simple pod definitions for the HTTP server
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				PodName:   destName,
				Namespace: destNs,
				Image:     fortioImageName,
				Command:   []string{"/usr/bin/fortio", "server", "-config-dir", "/etc/fortio", "--max-echo-delay", "24h"},
				Ports:     []int{fortioHTTPPort},
				OS:        Td.ClusterOS,
			})
		Expect(err).NotTo(HaveOccurred())

		_, err = Td.CreateServiceAccount(destNs, &svcAccDef)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreatePod(destNs, podDef)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreateService(destNs, svcDef)
		Expect(err).NotTo(HaveOccurred())

		// Expect it to be up and running in it's receiver namespace
		Expect(Td.WaitForPodsRunningReady(destNs, 60*time.Second, 1, nil)).To(Succeed())

		// Install nginx ingress controller
		ingressAddr, err := Td.InstallNginxIngress()
		Expect(err).ToNot((HaveOccurred()))

		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: svcDef.Name,
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/proxy-read-timeout": "360000", // 100 hours so it doesn't impact the test
				},
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: pointer.String("nginx"),
				Rules: []networkingv1.IngressRule{
					{
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									// Adding root path due to nginx ingress issue: https://github.com/kubernetes/ingress-nginx/issues/8518
									{
										Path:     "/",
										PathType: (*networkingv1.PathType)(pointer.String(string(networkingv1.PathTypePrefix))),
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: svcDef.Name,
												Port: networkingv1.ServiceBackendPort{
													Number: fortioHTTPPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = Td.Client.NetworkingV1().Ingresses(destNs).Create(context.Background(), ing, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		fortioTimeout := sidecarTimeout - 1

		// Requests should fail when no IngressBackend resource exists
		url := fmt.Sprintf("http://%s?delay=%ds", ingressAddr, fortioTimeout)

		// Source in the ingress backend must be added to the mesh for endpoint discovery
		Expect(Td.AddNsToMesh(false, NginxIngressSvc.Namespace)).To(Succeed())

		ingressBackend := &policyv1alpha1.IngressBackend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fortio-http",
				Namespace: destNs,
			},
			Spec: policyv1alpha1.IngressBackendSpec{
				Backends: []policyv1alpha1.BackendSpec{
					{
						Name: svcDef.Name,
						Port: policyv1alpha1.PortSpec{
							Number:   fortioHTTPPort,
							Protocol: "http",
						},
					},
				},
				Sources: []policyv1alpha1.IngressSourceSpec{
					{
						Kind:      "Service",
						Name:      NginxIngressSvc.Name,
						Namespace: NginxIngressSvc.Namespace,
					},
				},
			},
		}

		_, err = Td.PolicyClient.PolicyV1alpha1().IngressBackends(ingressBackend.Namespace).Create(context.TODO(), ingressBackend, metav1.CreateOptions{})
		Expect(err).ToNot((HaveOccurred()))

		// Expect ingress to reach server
		resp, err := http.Get(url) // #nosec G107: Potential HTTP request made with variable url
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}

		srcToDestStr := fmt.Sprintf("http ingress %s -> %s", ingressAddr, fmt.Sprintf("%s.%s", destName, destNs))

		Expect(err).NotTo(HaveOccurred(), "> (%s) HTTP Req failed with err %v", srcToDestStr, err)

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(200), "Failed testing HTTP idleTimeout of %ds with a fortio delay of %ds. Expected 200 received %d or err %v. Body was: %s", sidecarTimeout, fortioTimeout, status, err, body)
	})
}
