package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"
)

const nmapTool = "nmap"

// nmapParser 解析 `nmap -oX -` 的标准 XML 输出。
type nmapParser struct{}

// NmapXML 返回 nmap XML 解析器。
func NmapXML() Parser { return nmapParser{} }

func (nmapParser) Name() string { return nmapTool }

type xmlNmapRun struct {
	XMLName xml.Name  `xml:"nmaprun"`
	Hosts   []xmlHost `xml:"host"`
}

type xmlHost struct {
	Addresses []xmlAddress  `xml:"address"`
	Hostnames []xmlHostname `xml:"hostnames>hostname"`
	Ports     []xmlPort     `xml:"ports>port"`
}

type xmlAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type xmlHostname struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type xmlPort struct {
	Protocol string     `xml:"protocol,attr"`
	PortID   int        `xml:"portid,attr"`
	State    xmlState   `xml:"state"`
	Service  xmlService `xml:"service"`
}

type xmlState struct {
	State string `xml:"state,attr"`
}

type xmlService struct {
	Name    string   `xml:"name,attr"`
	Product string   `xml:"product,attr"`
	Version string   `xml:"version,attr"`
	CPEs    []string `xml:"cpe"`
}

func (nmapParser) Parse(stdout, stderr string) (Result, error) {
	res := Result{Tool: nmapTool, OK: false, Items: []Item{}}
	if err := checkSize(nmapTool, stdout); err != nil {
		res.Err = err.Error()
		return res, err
	}

	var run xmlNmapRun
	if err := xml.Unmarshal([]byte(stdout), &run); err != nil {
		err = fmt.Errorf("%s: XML 解析失败: %w", nmapTool, err)
		res.Err = err.Error()
		return res, err
	}

	for _, host := range run.Hosts {
		ip := pickIP(host.Addresses)
		name := pickHostname(host.Hostnames)
		if name == "" {
			name = ip
		}
		for _, port := range host.Ports {
			if !strings.EqualFold(port.State.State, "open") {
				continue
			}
			cpes := port.Service.CPEs
			if cpes == nil {
				cpes = []string{}
			}
			res.Items = append(res.Items, Item{
				Kind: "port",
				Data: map[string]any{
					"host":    name,
					"ip":      ip,
					"port":    port.PortID,
					"proto":   port.Protocol,
					"state":   strings.ToLower(port.State.State),
					"service": port.Service.Name,
					"product": port.Service.Product,
					"version": port.Service.Version,
					"cpe":     cpes,
				},
			})
		}
	}
	res.OK = true
	return res, nil
}

// pickIP 优先取 ipv4/ipv6 地址，其次退回任意地址。
func pickIP(addrs []xmlAddress) string {
	var fallback string
	for _, addr := range addrs {
		if fallback == "" && addr.Addr != "" {
			fallback = addr.Addr
		}
		switch strings.ToLower(addr.AddrType) {
		case "ipv4", "ipv6":
			if addr.Addr != "" {
				return addr.Addr
			}
		}
	}
	return fallback
}

func pickHostname(hostnames []xmlHostname) string {
	for _, h := range hostnames {
		if h.Name != "" {
			return h.Name
		}
	}
	return ""
}
