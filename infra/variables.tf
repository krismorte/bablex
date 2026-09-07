variable "aws_region" {
  type        = string
  description = "AWS region for the stack."
  default     = "eu-west-1"
}

variable "project_name" {
  type        = string
  description = "Project name used in resource names."
  default     = "bablex"
}

variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "dev"
}

variable "frontend_dist_dir" {
  type        = string
  description = "Path to the built React dist directory, relative to the infra directory."
  default     = "../frontend/dist"
}

variable "cognito_domain_prefix" {
  type        = string
  description = "Globally-unique Cognito hosted UI domain prefix. Lowercase letters, numbers and hyphens."
  default     = "bablex-demo"

  validation {
    condition     = var.cognito_domain_prefix != "replace-with-a-unique-prefix" && can(regex("^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$", var.cognito_domain_prefix))
    error_message = "Set cognito_domain_prefix to a real globally-unique value, for example bablex-yourname-2026; do not use replace-with-a-unique-prefix."
  }
}


variable "allowed_public_ipv4_cidrs" {
  type        = list(string)
  description = "Public IPv4 CIDR blocks allowed to access Bablex. Use a single /32 for one personal IP."

  validation {
    condition     = length(var.allowed_public_ipv4_cidrs) > 0 && alltrue([for cidr in var.allowed_public_ipv4_cidrs : can(cidrhost(cidr, 0))])
    error_message = "Provide at least one valid IPv4 CIDR block, for example 203.0.113.10/32."
  }
}


variable "cloudfront_api_shared_secret" {
  type        = string
  sensitive   = true
  description = "Reserved compatibility variable for existing terraform.auto.tfvars."
  default     = ""
}
