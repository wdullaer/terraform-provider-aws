package aws

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/hashicorp/terraform/helper/acctest"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func init() {
	resource.AddTestSweepers("aws_eks_cluster", &resource.Sweeper{
		Name: "aws_eks_cluster",
		F:    testSweepEksClusters,
	})
}

func testSweepEksClusters(region string) error {
	client, err := sharedClientForRegion(region)
	if err != nil {
		return fmt.Errorf("error getting client: %s", err)
	}
	conn := client.(*AWSClient).eksconn

	input := &eks.ListClustersInput{}
	for {
		out, err := conn.ListClusters(input)
		if err != nil {
			if testSweepSkipSweepError(err) {
				log.Printf("[WARN] Skipping EKS Clusters sweep for %s: %s", region, err)
				return nil
			}
			return fmt.Errorf("Error retrieving EKS Clusters: %s", err)
		}

		if out == nil || len(out.Clusters) == 0 {
			log.Printf("[INFO] No EKS Clusters to sweep")
			return nil
		}

		for _, cluster := range out.Clusters {
			name := aws.StringValue(cluster)

			log.Printf("[INFO] Deleting EKS Cluster: %s", name)
			err := deleteEksCluster(conn, name)
			if err != nil {
				log.Printf("[ERROR] Failed to delete EKS Cluster %s: %s", name, err)
				continue
			}
			err = waitForDeleteEksCluster(conn, name, 15*time.Minute)
			if err != nil {
				log.Printf("[ERROR] Failed to wait for EKS Cluster %s deletion: %s", name, err)
			}
		}

		if out.NextToken == nil {
			break
		}

		input.NextToken = out.NextToken
	}

	return nil
}

func TestAccAWSEksCluster_basic(t *testing.T) {
	var cluster eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_Required(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster),
					resource.TestMatchResourceAttr(resourceName, "arn", regexp.MustCompile(fmt.Sprintf("^arn:[^:]+:eks:[^:]+:[^:]+:cluster/%s$", rName))),
					resource.TestCheckResourceAttr(resourceName, "certificate_authority.#", "1"),
					resource.TestCheckResourceAttrSet(resourceName, "certificate_authority.0.data"),
					resource.TestMatchResourceAttr(resourceName, "endpoint", regexp.MustCompile(`^https://`)),
					resource.TestCheckResourceAttr(resourceName, "identity.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "identity.0.oidc.#", "1"),
					resource.TestMatchResourceAttr(resourceName, "identity.0.oidc.0.issuer", regexp.MustCompile(`^https://`)),
					resource.TestMatchResourceAttr(resourceName, "identity.0.oidc.0.thumbprint_list.0", regexp.MustCompile(`^[0-9a-f]{40}$`)),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestMatchResourceAttr(resourceName, "platform_version", regexp.MustCompile(`^eks\.\d+$`)),
					resource.TestMatchResourceAttr(resourceName, "role_arn", regexp.MustCompile(fmt.Sprintf("%s$", rName))),
					resource.TestCheckResourceAttr(resourceName, "status", eks.ClusterStatusActive),
					resource.TestMatchResourceAttr(resourceName, "version", regexp.MustCompile(`^\d+\.\d+$`)),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_private_access", "false"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_public_access", "true"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.security_group_ids.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.subnet_ids.#", "2"),
					resource.TestMatchResourceAttr(resourceName, "vpc_config.0.vpc_id", regexp.MustCompile(`^vpc-.+`)),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEksCluster_Version(t *testing.T) {
	var cluster1, cluster2 eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_Version(rName, "1.13"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster1),
					resource.TestCheckResourceAttr(resourceName, "version", "1.13"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccAWSEksClusterConfig_Version(rName, "1.14"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster2),
					testAccCheckAWSEksClusterNotRecreated(&cluster1, &cluster2),
					resource.TestCheckResourceAttr(resourceName, "version", "1.14"),
				),
			},
		},
	})
}

func TestAccAWSEksCluster_Logging(t *testing.T) {
	var cluster1, cluster2 eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_Logging(rName, []string{"api"}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster1),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.2902841359", "api"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccAWSEksClusterConfig_Logging(rName, []string{"api", "audit"}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster2),
					testAccCheckAWSEksClusterNotRecreated(&cluster1, &cluster2),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.#", "2"),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.2902841359", "api"),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.2451111801", "audit"),
				),
			},
			// Disable all log types.
			{
				Config: testAccAWSEksClusterConfig_Required(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster2),
					testAccCheckAWSEksClusterNotRecreated(&cluster1, &cluster2),
					resource.TestCheckResourceAttr(resourceName, "enabled_cluster_log_types.#", "0"),
				),
			},
		},
	})
}

func TestAccAWSEksCluster_VpcConfig_SecurityGroupIds(t *testing.T) {
	var cluster eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_SecurityGroupIds(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.security_group_ids.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEksCluster_VpcConfig_EndpointPrivateAccess(t *testing.T) {
	var cluster1, cluster2, cluster3 eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPrivateAccess(rName, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster1),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_private_access", "true"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPrivateAccess(rName, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster2),
					testAccCheckAWSEksClusterNotRecreated(&cluster1, &cluster2),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_private_access", "false"),
				),
			},
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPrivateAccess(rName, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster3),
					testAccCheckAWSEksClusterNotRecreated(&cluster2, &cluster3),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_private_access", "true"),
				),
			},
		},
	})
}

func TestAccAWSEksCluster_VpcConfig_EndpointPublicAccess(t *testing.T) {
	var cluster1, cluster2, cluster3 eks.Cluster

	rName := fmt.Sprintf("tf-acc-test-%s", acctest.RandString(5))
	resourceName := "aws_eks_cluster.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t); testAccPreCheckAWSEks(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSEksClusterDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPublicAccess(rName, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster1),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_public_access", "false"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPublicAccess(rName, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster2),
					testAccCheckAWSEksClusterNotRecreated(&cluster1, &cluster2),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_public_access", "true"),
				),
			},
			{
				Config: testAccAWSEksClusterConfig_VpcConfig_EndpointPublicAccess(rName, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEksClusterExists(resourceName, &cluster3),
					testAccCheckAWSEksClusterNotRecreated(&cluster2, &cluster3),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "vpc_config.0.endpoint_public_access", "false"),
				),
			},
		},
	})
}

func testAccCheckAWSEksClusterExists(resourceName string, cluster *eks.Cluster) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No EKS Cluster ID is set")
		}

		conn := testAccProvider.Meta().(*AWSClient).eksconn
		output, err := conn.DescribeCluster(&eks.DescribeClusterInput{
			Name: aws.String(rs.Primary.ID),
		})
		if err != nil {
			return err
		}

		if output == nil || output.Cluster == nil {
			return fmt.Errorf("EKS Cluster (%s) not found", rs.Primary.ID)
		}

		if aws.StringValue(output.Cluster.Name) != rs.Primary.ID {
			return fmt.Errorf("EKS Cluster (%s) not found", rs.Primary.ID)
		}

		*cluster = *output.Cluster

		return nil
	}
}

func testAccCheckAWSEksClusterDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_eks_cluster" {
			continue
		}

		conn := testAccProvider.Meta().(*AWSClient).eksconn

		// Handle eventual consistency
		err := resource.Retry(1*time.Minute, func() *resource.RetryError {
			output, err := conn.DescribeCluster(&eks.DescribeClusterInput{
				Name: aws.String(rs.Primary.ID),
			})

			if err != nil {
				if isAWSErr(err, eks.ErrCodeResourceNotFoundException, "") {
					return nil
				}
				return resource.NonRetryableError(err)
			}

			if output != nil && output.Cluster != nil && aws.StringValue(output.Cluster.Name) == rs.Primary.ID {
				return resource.RetryableError(fmt.Errorf("EKS Cluster %s still exists", rs.Primary.ID))
			}

			return nil
		})

		return err
	}

	return nil
}

func testAccCheckAWSEksClusterNotRecreated(i, j *eks.Cluster) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if aws.TimeValue(i.CreatedAt) != aws.TimeValue(j.CreatedAt) {
			return errors.New("EKS Cluster was recreated")
		}

		return nil
	}
}

func testAccPreCheckAWSEks(t *testing.T) {
	conn := testAccProvider.Meta().(*AWSClient).eksconn

	input := &eks.ListClustersInput{}

	_, err := conn.ListClusters(input)

	if testAccPreCheckSkipError(err) {
		t.Skipf("skipping acceptance testing: %s", err)
	}

	if err != nil {
		t.Fatalf("unexpected PreCheck error: %s", err)
	}
}

func testAccAWSEksClusterConfig_Base(rName string) string {
	return fmt.Sprintf(`
data "aws_availability_zones" "available" {}

resource "aws_iam_role" "test" {
  name = "%s"

  assume_role_policy = <<POLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "eks.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
POLICY
}

resource "aws_iam_role_policy_attachment" "test-AmazonEKSClusterPolicy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  role       = "${aws_iam_role.test.name}"
}

resource "aws_iam_role_policy_attachment" "test-AmazonEKSServicePolicy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSServicePolicy"
  role       = "${aws_iam_role.test.name}"
}

resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"

  tags = {
    Name                       = "terraform-testacc-eks-cluster-base"
    "kubernetes.io/cluster/%s" = "shared"
  }
}

resource "aws_subnet" "test" {
  count = 2

  availability_zone = "${data.aws_availability_zones.available.names[count.index]}"
  cidr_block        = "10.0.${count.index}.0/24"
  vpc_id            = "${aws_vpc.test.id}"

  tags = {
    Name                       = "terraform-testacc-eks-cluster-base"
    "kubernetes.io/cluster/%s" = "shared"
  }
}
`, rName, rName, rName)
}

func testAccAWSEksClusterConfig_Required(rName string) string {
	return fmt.Sprintf(`
%s

resource "aws_eks_cluster" "test" {
  name     = "%s"
  role_arn = "${aws_iam_role.test.arn}"

  vpc_config {
    subnet_ids = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName)
}

func testAccAWSEksClusterConfig_Version(rName, version string) string {
	return fmt.Sprintf(`
%s

resource "aws_eks_cluster" "test" {
  name     = "%s"
  role_arn = "${aws_iam_role.test.arn}"
  version  = "%s"

  vpc_config {
    subnet_ids = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName, version)
}

func testAccAWSEksClusterConfig_Logging(rName string, logTypes []string) string {
	return fmt.Sprintf(`
%s

resource "aws_eks_cluster" "test" {
  name                      = "%s"
  role_arn                  = "${aws_iam_role.test.arn}"
  enabled_cluster_log_types = ["%v"]

  vpc_config {
    subnet_ids = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName, strings.Join(logTypes, "\", \""))
}

func testAccAWSEksClusterConfig_VpcConfig_SecurityGroupIds(rName string) string {
	return fmt.Sprintf(`
%s

resource "aws_security_group" "test" {
  vpc_id = "${aws_vpc.test.id}"

  tags = {
    Name = "terraform-testacc-eks-cluster-sg"
  }
}

resource "aws_eks_cluster" "test" {
  name     = "%s"
  role_arn = "${aws_iam_role.test.arn}"

  vpc_config {
    security_group_ids = ["${aws_security_group.test.id}"]
    subnet_ids         = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName)
}

func testAccAWSEksClusterConfig_VpcConfig_EndpointPrivateAccess(rName string, endpointPrivateAccess bool) string {
	return fmt.Sprintf(`
%[1]s

resource "aws_eks_cluster" "test" {
  name     = %[2]q
  role_arn = "${aws_iam_role.test.arn}"

  vpc_config {
    endpoint_private_access = %[3]t
    endpoint_public_access  = true
    subnet_ids              = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName, endpointPrivateAccess)
}

func testAccAWSEksClusterConfig_VpcConfig_EndpointPublicAccess(rName string, endpointPublicAccess bool) string {
	return fmt.Sprintf(`
%[1]s

resource "aws_eks_cluster" "test" {
  name     = %[2]q
  role_arn = "${aws_iam_role.test.arn}"

  vpc_config {
    endpoint_private_access = true
    endpoint_public_access  = %[3]t
    subnet_ids              = ["${aws_subnet.test.*.id[0]}", "${aws_subnet.test.*.id[1]}"]
  }

  depends_on = ["aws_iam_role_policy_attachment.test-AmazonEKSClusterPolicy", "aws_iam_role_policy_attachment.test-AmazonEKSServicePolicy"]
}
`, testAccAWSEksClusterConfig_Base(rName), rName, endpointPublicAccess)
}
