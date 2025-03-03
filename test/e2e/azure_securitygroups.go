//go:build e2e
// +build e2e

/*
Copyright 2023 The Kubernetes Authors.

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

package e2e

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-08-01/network"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	azureutil "sigs.k8s.io/cluster-api-provider-azure/util/azure"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AzureSecurityGroupsSpecInput is the input for AzureSecurityGroupsSpec.
type AzureSecurityGroupsSpecInput struct {
	BootstrapClusterProxy framework.ClusterProxy
	Namespace             *corev1.Namespace
	ClusterName           string
	Cluster               *clusterv1.Cluster
	WaitForUpdate         []interface{}
}

func AzureSecurityGroupsSpec(ctx context.Context, inputGetter func() AzureSecurityGroupsSpecInput) {
	var (
		specName         = "azure-vmextensions"
		testSecurityRule = infrav1.SecurityRule{
			Name:             "test-security-rule",
			Description:      "test-security-rule",
			Protocol:         "Tcp",
			Direction:        "Outbound",
			Priority:         100,
			SourcePorts:      ptr.To("*"),
			DestinationPorts: ptr.To("80"),
			Source:           ptr.To("*"),
			Destination:      ptr.To("*"),
		}
		testSecurityRule2 = infrav1.SecurityRule{
			Name:             "test-security-rule-2",
			Description:      "test-security-rule-2",
			Protocol:         "Tcp",
			Direction:        "Inbound",
			Priority:         110,
			SourcePorts:      ptr.To("*"),
			DestinationPorts: ptr.To("80"),
			Source:           ptr.To("*"),
			Destination:      ptr.To("*"),
		}
		input AzureSecurityGroupsSpecInput
	)

	Expect(ctx).NotTo(BeNil(), "ctx is required for %s spec", specName)

	input = inputGetter()
	Expect(input.BootstrapClusterProxy).ToNot(BeNil(), "Invalid argument. input.BootstrapClusterProxy can't be nil when calling %s spec", specName)
	Expect(input.Namespace).ToNot(BeNil(), "Invalid argument. input.Namespace can't be nil when calling %s spec", specName)
	Expect(input.ClusterName).ToNot(BeEmpty(), "Invalid argument. input.ClusterName can't be empty when calling %s spec", specName)

	By("creating a Kubernetes client to the workload cluster")
	workloadClusterProxy := input.BootstrapClusterProxy.GetWorkloadCluster(ctx, input.Namespace.Name, input.ClusterName)
	Expect(workloadClusterProxy).NotTo(BeNil())
	mgmtClient := bootstrapClusterProxy.GetClient()
	Expect(mgmtClient).NotTo(BeNil())

	// get subscription id
	settings, err := auth.GetSettingsFromEnvironment()
	Expect(err).NotTo(HaveOccurred())
	subscriptionID := settings.GetSubscriptionID()
	auth, err := azureutil.GetAuthorizer(settings)
	Expect(err).NotTo(HaveOccurred())

	By("creating a subnets client")
	subnetsClient := network.NewSubnetsClient(subscriptionID)
	subnetsClient.Authorizer = auth

	securityGroupsClient := network.NewSecurityGroupsClient(subscriptionID)
	securityGroupsClient.Authorizer = auth

	securityRulesClient := network.NewSecurityRulesClient(subscriptionID)
	securityRulesClient.Authorizer = auth

	azureCluster := &infrav1.AzureCluster{}
	err = mgmtClient.Get(ctx, client.ObjectKey{
		Namespace: input.Cluster.Spec.InfrastructureRef.Namespace,
		Name:      input.Cluster.Spec.InfrastructureRef.Name,
	}, azureCluster)
	Expect(err).NotTo(HaveOccurred())

	var expectedSubnets infrav1.Subnets
	checkSubnets := func(g Gomega) {
		for _, expectedSubnet := range expectedSubnets {
			securityGroup, err := securityGroupsClient.Get(ctx, azureCluster.Spec.ResourceGroup, expectedSubnet.SecurityGroup.Name, "")
			g.Expect(err).NotTo(HaveOccurred())

			securityRules, err := securityRulesClient.List(ctx, azureCluster.Spec.ResourceGroup, *securityGroup.Name)
			g.Expect(err).NotTo(HaveOccurred())

			var expectedSecurityRuleNames []string
			for _, expectedSecurityRule := range expectedSubnet.SecurityGroup.SecurityRules {
				expectedSecurityRuleNames = append(expectedSecurityRuleNames, expectedSecurityRule.Name)
			}

			for _, securityRule := range securityRules.Values() {
				g.Expect(expectedSecurityRuleNames).To(ContainElement(*securityRule.Name))
			}
		}
	}

	Byf("Creating subnets for the %s cluster", input.ClusterName)
	testSubnet := infrav1.SubnetSpec{
		SubnetClassSpec: infrav1.SubnetClassSpec{
			Name: "test-subnet",
			Role: infrav1.SubnetNode,
		},
		SecurityGroup: infrav1.SecurityGroup{
			Name: "test-security-group",
			SecurityGroupClass: infrav1.SecurityGroupClass{
				SecurityRules: []infrav1.SecurityRule{
					testSecurityRule,
				},
			},
		},
	}
	originalSubnets := azureCluster.Spec.NetworkSpec.Subnets
	expectedSubnets = originalSubnets
	expectedSubnets = append(expectedSubnets, testSubnet)
	Eventually(func(g Gomega) {
		g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(azureCluster), azureCluster)).To(Succeed())
		azureCluster.Spec.NetworkSpec.Subnets = expectedSubnets
		g.Expect(mgmtClient.Update(ctx, azureCluster)).To(Succeed())
	}, inputGetter().WaitForUpdate...).Should(Succeed())
	Eventually(checkSubnets, input.WaitForUpdate...).Should(Succeed())

	By("Creating new security rule for the subnet")
	Expect(len(expectedSubnets)).To(Not(Equal(0)))
	testSubnet.SecurityGroup.SecurityRules = infrav1.SecurityRules{testSecurityRule, testSecurityRule2}
	expectedSubnets = originalSubnets
	expectedSubnets = append(expectedSubnets, testSubnet)
	Eventually(func(g Gomega) {
		g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(azureCluster), azureCluster)).To(Succeed())
		azureCluster.Spec.NetworkSpec.Subnets = expectedSubnets
		g.Expect(mgmtClient.Update(ctx, azureCluster)).To(Succeed())
	}, inputGetter().WaitForUpdate...).Should(Succeed())
	Eventually(checkSubnets, input.WaitForUpdate...).Should(Succeed())

	By("Deleting security rule from the subnet")
	Expect(len(expectedSubnets)).To(Not(Equal(0)))
	testSubnet.SecurityGroup.SecurityRules = infrav1.SecurityRules{testSecurityRule2}
	expectedSubnets = originalSubnets
	expectedSubnets = append(expectedSubnets, testSubnet)
	Eventually(func(g Gomega) {
		g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(azureCluster), azureCluster)).To(Succeed())
		azureCluster.Spec.NetworkSpec.Subnets = expectedSubnets
		g.Expect(mgmtClient.Update(ctx, azureCluster)).To(Succeed())
	}, inputGetter().WaitForUpdate...).Should(Succeed())
	Eventually(checkSubnets, input.WaitForUpdate...).Should(Succeed())

	Byf("Deleting test subnet for the %s cluster", input.ClusterName)
	Eventually(func(g Gomega) {
		g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(azureCluster), azureCluster)).To(Succeed())
		azureCluster.Spec.NetworkSpec.Subnets = originalSubnets
		g.Expect(mgmtClient.Update(ctx, azureCluster)).To(Succeed())
	}, inputGetter().WaitForUpdate...).Should(Succeed())
	Eventually(checkSubnets, input.WaitForUpdate...).Should(Succeed())
}
