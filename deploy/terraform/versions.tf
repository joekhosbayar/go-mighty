terraform {
  required_version = ">= 1.10"

  backend "s3" {
    bucket       = "mighty-tfstate-711387141487"
    key          = "mvp/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = { Project = "mighty" }
  }
}

data "aws_caller_identity" "current" {}
