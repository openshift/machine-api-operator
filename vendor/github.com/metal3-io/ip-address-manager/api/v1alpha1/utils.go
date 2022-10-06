/*
Copyright 2019 The Kubernetes Authors.

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

package v1alpha1

import (
	"fmt"
	"math/big"
	"net"

	"github.com/pkg/errors"
)

// GetIPAddress renders the IP address, taking the index, offset and step into
// account, it is IP version agnostic.
func GetIPAddress(entry Pool, index int) (IPAddressStr, error) {
	if entry.Start == nil && entry.Subnet == nil {
		return "", errors.New("Either Start or Subnet is required for ipAddress")
	}
	var ip net.IP
	var err error
	var ipNet *net.IPNet
	offset := index

	// If start is given, use it to add the offset.
	if entry.Start != nil {
		var endIP net.IP
		if entry.End != nil {
			endIP = net.ParseIP(string(*entry.End))
		}
		ip, err = addOffsetToIP(net.ParseIP(string(*entry.Start)), endIP, offset)
		if err != nil {
			return "", err
		}

		// Verify that the IP is in the subnet.
		if entry.Subnet != nil {
			_, ipNet, err = net.ParseCIDR(string(*entry.Subnet))
			if err != nil {
				return "", err
			}
			if !ipNet.Contains(ip) {
				return "", errors.New("IP address out of bonds")
			}
		}

		// If it is not given, use the CIDR ip address and increment the offset by 1.
	} else {
		ip, ipNet, err = net.ParseCIDR(string(*entry.Subnet))
		if err != nil {
			return "", err
		}
		offset++
		ip, err = addOffsetToIP(ip, nil, offset)
		if err != nil {
			return "", err
		}

		// Verify that the ip is in the subnet.
		if !ipNet.Contains(ip) {
			return "", errors.New("IP address out of bonds")
		}
	}
	return IPAddressStr(ip.String()), nil
}

// addOffsetToIP computes the value of the IP address with the offset. It is
// IP version agnostic
// Note that if the resulting IP address is in the format ::ffff:xxxx:xxxx then
// ip.String will fail to select the correct type of ip.
func addOffsetToIP(ip, endIP net.IP, offset int) (net.IP, error) {
	ip4 := false
	if ip.To4() != nil {
		ip4 = true
	}

	// Create big integers.
	IPInt := big.NewInt(0)
	OffsetInt := big.NewInt(int64(offset))

	// Transform the ip into an int. (big endian function).
	IPInt = IPInt.SetBytes(ip)

	// add the two integers.
	IPInt = IPInt.Add(IPInt, OffsetInt)

	// return the bytes list.
	IPBytes := IPInt.Bytes()

	IPBytesLen := len(IPBytes)

	// Verify that the IPv4 or IPv6 fulfills theirs constraints.
	if (ip4 && IPBytesLen > 6 && IPBytes[4] != 255 && IPBytes[5] != 255) ||
		(!ip4 && IPBytesLen > 16) {
		return nil, errors.New(fmt.Sprintf("IP address overflow for : %s", ip.String()))
	}

	// transform the end ip into an Int to compare.
	if endIP != nil {
		endIPInt := big.NewInt(0)
		endIPInt = endIPInt.SetBytes(endIP)
		// Computed IP is higher than the end IP.
		if IPInt.Cmp(endIPInt) > 0 {
			return nil, errors.New(fmt.Sprintf("IP address out of bonds for : %s", ip.String()))
		}
	}

	// COpy the output back into an ip.
	copy(ip[16-IPBytesLen:], IPBytes)
	return ip, nil
}
