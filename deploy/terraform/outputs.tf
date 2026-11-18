output "public_ip" {
  value = aws_instance.k3s.public_ip
}

output "kubeconfig_steps" {
  value = <<-EOT
    # wait ~2 min for k3s to install, then:
    scp ubuntu@${aws_instance.k3s.public_ip}:/etc/rancher/k3s/k3s.yaml ./kubeconfig
    sed -i.bak 's/127.0.0.1/${aws_instance.k3s.public_ip}/' ./kubeconfig
    export KUBECONFIG=$PWD/kubeconfig
    kubectl get nodes
  EOT
}
