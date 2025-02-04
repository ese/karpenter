/*
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

package aws

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CreateFleet Batching", func() {
	var cfb *CreateFleetBatcher

	BeforeEach(func() {
		fakeEC2API.Reset()
		cfb = NewCreateFleetBatcher(ctx, fakeEC2API)
	})

	It("should batch the same inputs into a single call", func() {
		input := &ec2.CreateFleetInput{
			LaunchTemplateConfigs: []*ec2.FleetLaunchTemplateConfigRequest{
				{
					LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecificationRequest{
						LaunchTemplateName: aws.String("my-template"),
					},
					Overrides: []*ec2.FleetLaunchTemplateOverridesRequest{
						{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			TargetCapacitySpecification: &ec2.TargetCapacitySpecificationRequest{
				TotalTargetCapacity: aws.Int64(1),
			},
		}
		var wg sync.WaitGroup
		var receivedInstance int64
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				rsp, err := cfb.CreateFleet(ctx, input)
				Expect(err).To(BeNil())

				var instanceIds []string
				for _, rsv := range rsp.Instances {
					for _, id := range rsv.InstanceIds {
						instanceIds = append(instanceIds, *id)
					}
				}
				atomic.AddInt64(&receivedInstance, 1)
				Expect(instanceIds).To(HaveLen(1))
			}()
		}
		wg.Wait()

		Expect(receivedInstance).To(BeNumerically("==", 5))
		Expect(fakeEC2API.CalledWithCreateFleetInput.Len()).To(BeNumerically("==", 1))
		call := fakeEC2API.CalledWithCreateFleetInput.Pop()
		Expect(*call.TargetCapacitySpecification.TotalTargetCapacity).To(BeNumerically("==", 5))
	})
	It("should batch different inputs into multiple calls", func() {
		east1input := &ec2.CreateFleetInput{
			LaunchTemplateConfigs: []*ec2.FleetLaunchTemplateConfigRequest{
				{
					LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecificationRequest{
						LaunchTemplateName: aws.String("my-template"),
					},
					Overrides: []*ec2.FleetLaunchTemplateOverridesRequest{
						{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			TargetCapacitySpecification: &ec2.TargetCapacitySpecificationRequest{
				TotalTargetCapacity: aws.Int64(1),
			},
		}
		east2input := &ec2.CreateFleetInput{
			LaunchTemplateConfigs: []*ec2.FleetLaunchTemplateConfigRequest{
				{
					LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecificationRequest{
						LaunchTemplateName: aws.String("my-template"),
					},
					Overrides: []*ec2.FleetLaunchTemplateOverridesRequest{
						{
							AvailabilityZone: aws.String("us-east-2"),
						},
					},
				},
			},
			TargetCapacitySpecification: &ec2.TargetCapacitySpecificationRequest{
				TotalTargetCapacity: aws.Int64(1),
			},
		}
		var wg sync.WaitGroup
		var receivedInstance int64
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(i int) {
				defer GinkgoRecover()
				defer wg.Done()
				input := east1input
				// 4 instances for us-east-1 and 1 instance in us-east-2
				if i == 3 {
					input = east2input
				}
				rsp, err := cfb.CreateFleet(ctx, input)
				Expect(err).To(BeNil())

				var instanceIds []string
				for _, rsv := range rsp.Instances {
					for _, id := range rsv.InstanceIds {
						instanceIds = append(instanceIds, *id)
					}
				}
				atomic.AddInt64(&receivedInstance, 1)
				Expect(instanceIds).To(HaveLen(1))
				time.Sleep(100 * time.Millisecond)
			}(i)
		}
		wg.Wait()

		Expect(receivedInstance).To(BeNumerically("==", 5))
		Expect(fakeEC2API.CalledWithCreateFleetInput.Len()).To(BeNumerically("==", 2))
		east2Call := fakeEC2API.CalledWithCreateFleetInput.Pop()
		east1Call := fakeEC2API.CalledWithCreateFleetInput.Pop()
		if *east2Call.TargetCapacitySpecification.TotalTargetCapacity > *east1Call.TargetCapacitySpecification.TotalTargetCapacity {
			east2Call, east1Call = east1Call, east2Call
		}
		Expect(*east2Call.TargetCapacitySpecification.TotalTargetCapacity).To(BeNumerically("==", 1))
		Expect(*east2Call.LaunchTemplateConfigs[0].Overrides[0].AvailabilityZone).To(Equal("us-east-2"))
		Expect(*east1Call.TargetCapacitySpecification.TotalTargetCapacity).To(BeNumerically("==", 4))
		Expect(*east1Call.LaunchTemplateConfigs[0].Overrides[0].AvailabilityZone).To(Equal("us-east-1"))
	})
	It("should return any errors to callers", func() {
		input := &ec2.CreateFleetInput{
			LaunchTemplateConfigs: []*ec2.FleetLaunchTemplateConfigRequest{
				{
					LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecificationRequest{
						LaunchTemplateName: aws.String("my-template"),
					},
					Overrides: []*ec2.FleetLaunchTemplateOverridesRequest{
						{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			TargetCapacitySpecification: &ec2.TargetCapacitySpecificationRequest{
				TotalTargetCapacity: aws.Int64(1),
			},
		}

		fakeEC2API.CreateFleetOutput.Set(&ec2.CreateFleetOutput{
			Errors: []*ec2.CreateFleetError{
				{
					ErrorCode:    aws.String("some-error"),
					ErrorMessage: aws.String("some-error"),
					LaunchTemplateAndOverrides: &ec2.LaunchTemplateAndOverridesResponse{
						LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecification{
							LaunchTemplateName: aws.String("my-template"),
						},
						Overrides: &ec2.FleetLaunchTemplateOverrides{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
				{
					ErrorCode:    aws.String("some-other-error"),
					ErrorMessage: aws.String("some-other-error"),
					LaunchTemplateAndOverrides: &ec2.LaunchTemplateAndOverridesResponse{
						LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecification{
							LaunchTemplateName: aws.String("my-template"),
						},
						Overrides: &ec2.FleetLaunchTemplateOverrides{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			FleetId: aws.String("some-id"),
			Instances: []*ec2.CreateFleetInstance{
				{
					InstanceIds:                []*string{aws.String("id-1"), aws.String("id-2"), aws.String("id-3"), aws.String("id-4"), aws.String("id-5")},
					InstanceType:               nil,
					LaunchTemplateAndOverrides: nil,
					Lifecycle:                  nil,
					Platform:                   nil,
				},
			},
		})
		var wg sync.WaitGroup
		var receivedInstance int64
		var numErrors int64
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				rsp, err := cfb.CreateFleet(ctx, input)
				Expect(err).To(BeNil())

				if len(rsp.Errors) != 0 {
					// should receive errors for each caller
					atomic.AddInt64(&numErrors, 1)
				}

				var instanceIds []string
				for _, rsv := range rsp.Instances {
					for _, id := range rsv.InstanceIds {
						instanceIds = append(instanceIds, *id)
					}
				}
				atomic.AddInt64(&receivedInstance, 1)
				Expect(instanceIds).To(HaveLen(1))
			}()
		}
		wg.Wait()

		Expect(fakeEC2API.CalledWithCreateFleetInput.Len()).To(BeNumerically("==", 1))
		call := fakeEC2API.CalledWithCreateFleetInput.Pop()
		// requested 5 instances
		Expect(*call.TargetCapacitySpecification.TotalTargetCapacity).To(BeNumerically("==", 5))
		// but got three instances and two failures
		Expect(receivedInstance).To(BeNumerically("==", 5))
		Expect(numErrors).To(BeNumerically("==", 5))
	})
	It("should handle partial fulfillment", func() {
		input := &ec2.CreateFleetInput{
			LaunchTemplateConfigs: []*ec2.FleetLaunchTemplateConfigRequest{
				{
					LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecificationRequest{
						LaunchTemplateName: aws.String("my-template"),
					},
					Overrides: []*ec2.FleetLaunchTemplateOverridesRequest{
						{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			TargetCapacitySpecification: &ec2.TargetCapacitySpecificationRequest{
				TotalTargetCapacity: aws.Int64(1),
			},
		}

		fakeEC2API.CreateFleetOutput.Set(&ec2.CreateFleetOutput{
			Errors: []*ec2.CreateFleetError{
				{
					ErrorCode:    aws.String("some-error"),
					ErrorMessage: aws.String("some-error"),
					LaunchTemplateAndOverrides: &ec2.LaunchTemplateAndOverridesResponse{
						LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecification{
							LaunchTemplateName: aws.String("my-template"),
						},
						Overrides: &ec2.FleetLaunchTemplateOverrides{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
				{
					ErrorCode:    aws.String("some-other-error"),
					ErrorMessage: aws.String("some-other-error"),
					LaunchTemplateAndOverrides: &ec2.LaunchTemplateAndOverridesResponse{
						LaunchTemplateSpecification: &ec2.FleetLaunchTemplateSpecification{
							LaunchTemplateName: aws.String("my-template"),
						},
						Overrides: &ec2.FleetLaunchTemplateOverrides{
							AvailabilityZone: aws.String("us-east-1"),
						},
					},
				},
			},
			FleetId: aws.String("some-id"),
			Instances: []*ec2.CreateFleetInstance{
				{
					InstanceIds:                []*string{aws.String("id-1"), aws.String("id-2"), aws.String("id-3")},
					InstanceType:               nil,
					LaunchTemplateAndOverrides: nil,
					Lifecycle:                  nil,
					Platform:                   nil,
				},
			},
		})
		var wg sync.WaitGroup
		var receivedInstance int64
		var numErrors int64
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				rsp, err := cfb.CreateFleet(ctx, input)
				// partial fulfillment shouldn't cause an error at the CreateFleet call
				Expect(err).To(BeNil())

				if len(rsp.Errors) != 0 {
					atomic.AddInt64(&numErrors, 1)
				}

				var instanceIds []string
				for _, rsv := range rsp.Instances {
					for _, id := range rsv.InstanceIds {
						instanceIds = append(instanceIds, *id)
					}
				}
				Expect(instanceIds).To(Or(HaveLen(0), HaveLen(1)))
				if len(instanceIds) == 1 {
					atomic.AddInt64(&receivedInstance, 1)
				}
			}()
		}
		wg.Wait()

		Expect(fakeEC2API.CalledWithCreateFleetInput.Len()).To(BeNumerically("==", 1))
		call := fakeEC2API.CalledWithCreateFleetInput.Pop()
		// requested 5 instances
		Expect(*call.TargetCapacitySpecification.TotalTargetCapacity).To(BeNumerically("==", 5))
		// but got three instances and the errors were returned to all five calls
		Expect(receivedInstance).To(BeNumerically("==", 3))
		Expect(numErrors).To(BeNumerically("==", 5))
	})
})
