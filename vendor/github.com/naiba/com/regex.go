package com

import "regexp"

//IsDomain is valid domain
func IsDomain(domain string) bool {
	is, _ := regexp.Match(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,6}$`, []byte(domain))
	return is
}

//IsIPv4 is valid ipv4
func IsIPv4(ipv4 string) bool {
	is, _ := regexp.Match(`^(2[0-5]{2}|2[0-4][0-9]|1?[0-9]{1,2}).(2[0-5]{2}|2[0-4][0-9]|1?[0-9]{1,2}).(2[0-5]{2}|2[0-4][0-9]|1?[0-9]{1,2}).(2[0-5]{2}|2[0-4][0-9]|1?[0-9]{1,2})$`, []byte(ipv4))
	return is
}
