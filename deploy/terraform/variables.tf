variable "region" {
  type    = string
  default = "ca-central-1"
}

variable "my_ip" {
  type        = string
  description = "your public ip, ssh and 6443 are only open to this (curl ifconfig.me)"
}

variable "ssh_public_key_path" {
  type    = string
  default = "~/.ssh/id_ed25519.pub"
}

variable "budget_email" {
  type        = string
  default     = ""
  description = "leave empty to skip the budget alert"
}
