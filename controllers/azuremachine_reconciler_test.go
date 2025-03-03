/*
Copyright 2022 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2021-11-01/compute"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/mock_azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/resourceskus"
	gomockinternal "sigs.k8s.io/cluster-api-provider-azure/internal/test/matchers/gomock"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func TestAzureMachineServiceReconcile(t *testing.T) {
	cases := map[string]struct {
		expectedError string
		expect        func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder)
	}{
		"all services are reconciled in order": {
			expectedError: "",
			expect: func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder) {
				gomock.InOrder(
					one.Reconcile(gomockinternal.AContext()).Return(nil),
					two.Reconcile(gomockinternal.AContext()).Return(nil),
					three.Reconcile(gomockinternal.AContext()).Return(nil))
			},
		},
		"service reconcile fails": {
			expectedError: "failed to reconcile AzureMachine service foo: some error happened",
			expect: func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder) {
				gomock.InOrder(
					one.Reconcile(gomockinternal.AContext()).Return(nil),
					two.Reconcile(gomockinternal.AContext()).Return(errors.New("some error happened")),
					two.Name().Return("foo"))
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			t.Parallel()
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			svcOneMock := mock_azure.NewMockServiceReconciler(mockCtrl)
			svcTwoMock := mock_azure.NewMockServiceReconciler(mockCtrl)
			svcThreeMock := mock_azure.NewMockServiceReconciler(mockCtrl)

			tc.expect(svcOneMock.EXPECT(), svcTwoMock.EXPECT(), svcThreeMock.EXPECT())

			s := &azureMachineService{
				scope: &scope.MachineScope{
					ClusterScoper: &scope.ClusterScope{
						AzureCluster: &infrav1.AzureCluster{},
						Cluster:      &clusterv1.Cluster{},
					},
					Machine: &clusterv1.Machine{},
					AzureMachine: &infrav1.AzureMachine{
						Spec: infrav1.AzureMachineSpec{
							SubnetName: "test-subnet",
						},
					},
				},
				services: []azure.ServiceReconciler{
					svcOneMock,
					svcTwoMock,
					svcThreeMock,
				},
				skuCache: resourceskus.NewStaticCache([]compute.ResourceSku{}, ""),
			}

			err := s.reconcile(context.TODO())
			if tc.expectedError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestAzureMachineServicePause(t *testing.T) {
	type pausingServiceReconciler struct {
		*mock_azure.MockServiceReconciler
		*mock_azure.MockPauser
	}

	cases := map[string]struct {
		expectedError string
		expect        func(one pausingServiceReconciler, two pausingServiceReconciler, three pausingServiceReconciler)
	}{
		"all services are paused in order": {
			expectedError: "",
			expect: func(one pausingServiceReconciler, two pausingServiceReconciler, three pausingServiceReconciler) {
				gomock.InOrder(
					one.MockPauser.EXPECT().Pause(gomockinternal.AContext()).Return(nil),
					two.MockPauser.EXPECT().Pause(gomockinternal.AContext()).Return(nil),
					three.MockPauser.EXPECT().Pause(gomockinternal.AContext()).Return(nil))
			},
		},
		"service pause fails": {
			expectedError: "failed to pause AzureMachine service two: some error happened",
			expect: func(one pausingServiceReconciler, two pausingServiceReconciler, _ pausingServiceReconciler) {
				gomock.InOrder(
					one.MockPauser.EXPECT().Pause(gomockinternal.AContext()).Return(nil),
					two.MockPauser.EXPECT().Pause(gomockinternal.AContext()).Return(errors.New("some error happened")),
					two.MockServiceReconciler.EXPECT().Name().Return("two"))
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			t.Parallel()
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			newPausingServiceReconciler := func() pausingServiceReconciler {
				return pausingServiceReconciler{
					mock_azure.NewMockServiceReconciler(mockCtrl),
					mock_azure.NewMockPauser(mockCtrl),
				}
			}
			svcOneMock := newPausingServiceReconciler()
			svcTwoMock := newPausingServiceReconciler()
			svcThreeMock := newPausingServiceReconciler()

			tc.expect(svcOneMock, svcTwoMock, svcThreeMock)

			s := &azureMachineService{
				services: []azure.ServiceReconciler{
					svcOneMock,
					svcTwoMock,
					svcThreeMock,
				},
			}

			err := s.pause(context.TODO())
			if tc.expectedError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestAzureMachineServiceDelete(t *testing.T) {
	cases := map[string]struct {
		expectedError string
		expect        func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder)
	}{
		"all services deleted in order": {
			expectedError: "",
			expect: func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder) {
				gomock.InOrder(
					three.Delete(gomockinternal.AContext()).Return(nil),
					two.Delete(gomockinternal.AContext()).Return(nil),
					one.Delete(gomockinternal.AContext()).Return(nil))
			},
		},
		"service delete fails": {
			expectedError: "failed to delete AzureMachine service test-service-two: some error happened",
			expect: func(one *mock_azure.MockServiceReconcilerMockRecorder, two *mock_azure.MockServiceReconcilerMockRecorder, three *mock_azure.MockServiceReconcilerMockRecorder) {
				gomock.InOrder(
					three.Delete(gomockinternal.AContext()).Return(nil),
					two.Delete(gomockinternal.AContext()).Return(errors.New("some error happened")),
					two.Name().Return("test-service-two"))
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			t.Parallel()
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			svcOneMock := mock_azure.NewMockServiceReconciler(mockCtrl)
			svcTwoMock := mock_azure.NewMockServiceReconciler(mockCtrl)
			svcThreeMock := mock_azure.NewMockServiceReconciler(mockCtrl)

			tc.expect(svcOneMock.EXPECT(), svcTwoMock.EXPECT(), svcThreeMock.EXPECT())

			s := &azureMachineService{
				scope: &scope.MachineScope{
					ClusterScoper: &scope.ClusterScope{
						AzureCluster: &infrav1.AzureCluster{},
						Cluster:      &clusterv1.Cluster{},
					},
					Machine:      &clusterv1.Machine{},
					AzureMachine: &infrav1.AzureMachine{},
				},
				services: []azure.ServiceReconciler{
					svcOneMock,
					svcTwoMock,
					svcThreeMock,
				},
				skuCache: resourceskus.NewStaticCache([]compute.ResourceSku{}, ""),
			}

			err := s.delete(context.TODO())
			if tc.expectedError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
