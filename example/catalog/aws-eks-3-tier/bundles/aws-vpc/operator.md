# aws-vpc

{{#resources.network}}
## Inspect the network

```sh
aws ec2 describe-vpcs --vpc-ids {{resources.network.id}} --region {{params.region}}
aws ec2 describe-subnets --filters Name=vpc-id,Values={{resources.network.id}} --region {{params.region}} \
  --query 'Subnets[].{id:SubnetId,cidr:CidrBlock,az:AvailabilityZone}' --output table
```

## Private subnets cannot reach the internet

Check that a NAT gateway exists and its route is present:

```sh
aws ec2 describe-nat-gateways --filter Name=vpc-id,Values={{resources.network.id}} --region {{params.region}}
aws ec2 describe-route-tables --filters Name=vpc-id,Values={{resources.network.id}} --region {{params.region}} \
  --query 'RouteTables[].Routes[?DestinationCidrBlock==`0.0.0.0/0`]'
```

`nat_gateways: none` is a deliberate choice; switch the param to `single` and redeploy to add egress.
{{/resources.network}}
