data "aws_vpc" "default" {
  default = true
}

# Not every AZ in a region supports every instance type (e.g. t4g.small is
# unavailable in us-east-1e). Restrict candidate subnets to AZs that actually
# offer var.instance_type so subnet selection below can't land on a
# non-offering AZ and fail RunInstances.
data "aws_ec2_instance_type_offerings" "selected" {
  filter {
    name   = "instance-type"
    values = [var.instance_type]
  }
  location_type = "availability-zone"
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }

  filter {
    name   = "availability-zone"
    values = data.aws_ec2_instance_type_offerings.selected.locations
  }
}

resource "aws_security_group" "api" {
  name        = "mighty-api"
  description = "Caddy ingress: HTTPS + ACME HTTP only. No SSH."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTPS + WSS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTP for ACME challenges and HTTPS redirect"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
