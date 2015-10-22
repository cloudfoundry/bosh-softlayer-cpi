package vm

type Networks map[string]Network

type Network struct {
	Type string

	IP      string
	Netmask string
	Gateway string

	DNS     []string
	Default []string

	Preconfigured bool

	CloudProperties map[string]interface{}
}

func (ns Networks) First() Network {
	for _, net := range ns {
		return net
	}

	return Network{}
}

func (n Network) IsDynamic() bool { return n.Type == "dynamic" }

func (n Network) AppendDNS(dns string) Network {
	if len(dns) > 0 {
		n.DNS = append(n.DNS, dns)
		return n
	}
	return n
}
