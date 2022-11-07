#ifndef __LIB_GOOGLE_SFC_H_
#define __LIB_GOOGLE_SFC_H_

#include "common.h"
#include "google_maps.h"
#include "google_geneve.h"
#include "google_nsh.h"

#ifdef ENABLE_GOOGLE_SERVICE_STEERING

/* Can't use standard Geneve port 6081 because UDP traffic from pods on this port is dropped by Cilium. */
#define GOOGLE_SFC_RESERVED_PORT 7081

/* SFC_CIDR_STATIC_PREFIX represents the size in bits of the static prefix part of an SFC cidr key. */
#define SFC_CIDR_STATIC_PREFIX (sizeof(__u16) * 2 * 8)
#define SFC_CIDR_PREFIX_LEN(PREFIX) (SFC_CIDR_STATIC_PREFIX + (PREFIX))
#define SFC_CIDR_IPV4_PREFIX SFC_CIDR_PREFIX_LEN(32)

struct l4hdr {
	__be16 sport;
	__be16 dport;
};

struct flow_5tuple {
	__be32 saddr;
	__be32 daddr;
	__be16 sport;
	__be16 dport;
	__u8   protocol;
	__u8   r0;
};

struct encaphdr {
	struct iphdr ip;
	struct udphdr udp;
	struct genevehdr geneve;
	struct nshhdr nsh;
} __packed;

static __always_inline bool is_sfc_encapped(struct __ctx_buff *ctx, const struct iphdr *ip4) {
	__be16 dport;
	int l4_off = ETH_HLEN + ipv4_hdrlen(ip4);
	int dport_off = l4_off + UDP_DPORT_OFF;

	if (ip4->protocol != IPPROTO_UDP)
		return false;
	if (IS_ERR(l4_load_port(ctx, dport_off, &dport)))
		return false;
	return dport == bpf_htons(GOOGLE_SFC_RESERVED_PORT);
}

static __always_inline void set_ipv4_csum(struct iphdr *iph) {
	__u16 *iph16 = (__u16 *)iph;
	__u32 csum = 0;
	unsigned long i;
	iph->check = 0;
#pragma unroll
	for (i = 0; i < sizeof(*iph) >> 1; i++)
		csum += *iph16++;
	iph->check = ~((csum & 0xffff) + (csum >> 16));
}

/* IP hash for 5-tuple */
static __always_inline __u16 flow_id(const struct iphdr *ip4, const struct l4hdr *l4) {
	struct flow_5tuple flow = {
		.saddr = ip4->saddr,
		.daddr = ip4->daddr,
		.sport = l4->sport,
		.dport = l4->dport,
		.protocol = ip4->protocol,
	};
	__u16 *iph16 = (__u16 *)&flow;
	__u32 csum = 0;
	unsigned long i;
#pragma unroll
	for (i = 0; i < sizeof(flow) >> 1; i++)
		csum += *iph16++;
	return ~((csum & 0xffff) + (csum >> 16));
}

static __always_inline int sfc_encap(struct __ctx_buff *ctx, struct iphdr *ip4, struct sfc_path_key *key) {
	__u64 flags;
	int l4_off = ETH_HLEN + ipv4_hdrlen(ip4);
	struct l4hdr l4;
	struct encaphdr h_outer = {0};
	struct sfc_path_entry *path_entry;

	if (ctx_load_bytes(ctx, l4_off, &l4, sizeof(struct l4hdr)) < 0)
		return DROP_INVALID;

	path_entry = map_lookup_elem(&SFC_PATH_MAP, key);
	/* (SPI, SI) lookup should not fail. */
	if (path_entry == NULL)
		return DROP_NO_SERVICE;

	geneve_init(&h_outer.geneve, ETH_P_NSH);
	nsh_init(&h_outer.nsh, key->path);

	/* https://datatracker.ietf.org/doc/html/rfc8926#section-3.3:
	 * To encourage an even distribution of flows across multiple links, the source port SHOULD be
	 * calculated using a hash of the encapsulated packet headers using, for example, a traditional 5-tuple.
	 *
	 * The hash is ORed with 0x8000 to make the port high enough to not conflict with priveleged ports.
	 */
	h_outer.udp.source = bpf_htons(flow_id(ip4, &l4) | 0x8000);
	h_outer.udp.dest = bpf_htons(GOOGLE_SFC_RESERVED_PORT);
	h_outer.udp.len = bpf_htons(bpf_ntohs(ip4->tot_len) + sizeof(struct encaphdr) - sizeof(struct iphdr));

	h_outer.ip = *ip4;
	h_outer.ip.saddr = LXC_IPV4;
	h_outer.ip.daddr = path_entry->address;
	h_outer.ip.protocol = IPPROTO_UDP;
	h_outer.ip.ihl = sizeof(struct iphdr) >> 2;
	h_outer.ip.tot_len = bpf_htons(bpf_ntohs(ip4->tot_len) + sizeof(struct encaphdr));

	set_ipv4_csum(&h_outer.ip);

	flags = BPF_F_ADJ_ROOM_FIXED_GSO | BPF_F_ADJ_ROOM_ENCAP_L3_IPV4 | BPF_F_ADJ_ROOM_ENCAP_L4_UDP;
	if (ctx_adjust_hroom(ctx, sizeof(struct encaphdr), BPF_ADJ_ROOM_MAC, flags))
		return DROP_INVALID;
	if (ctx_store_bytes(ctx, ETH_HLEN, &h_outer, sizeof(struct encaphdr), BPF_F_INVALIDATE_HASH) < 0)
		return DROP_INVALID;

	return CTX_ACT_OK;
}

static __always_inline int sfc_decap(struct __ctx_buff *ctx, struct encaphdr *h_outer) {
	if (ctx_load_bytes(ctx, ETH_HLEN, h_outer, sizeof(struct encaphdr)) < 0)
		return DROP_INVALID;
	if (ctx_adjust_hroom(ctx, -(__s32)sizeof(struct encaphdr), BPF_ADJ_ROOM_MAC, BPF_F_ADJ_ROOM_FIXED_GSO))
		return DROP_INVALID;
	return CTX_ACT_OK;
}

static __always_inline __u8 lookup_prefix_len(__u32 ip, bool is_egress, bool is_dst) {
	struct sfc_cidr_key cidr_key = {
		.lpm_key = { SFC_CIDR_IPV4_PREFIX, {} },
		.ep_id = LXC_ID,
		.is_egress = is_egress,
		.is_dst = is_dst,
		.cidr = ip,
	};
	struct sfc_cidr_entry *cidr_entry = map_lookup_elem(&SFC_CIDR_MAP, &cidr_key);
	if (cidr_entry != NULL) {
		return cidr_entry->prefix_len;
	}
	/* Assume 0.0.0.0/0 if no prefix match */
	return 0;
}

static __always_inline __u32 mask_ipv4(__u32 ip, __u8 prefix_len) {
	__u32 mask = 0xFFFFFFFFu << (32 - prefix_len);
	if (prefix_len == 0)
		mask = 0;
	return ip & bpf_htonl(mask);
}

/**
 * Evaluate service steering traffic selection rules against packet.
 * @arg ctx:       Packet
 * @arg ip4:       Pointer to L3 header
 * @arg is_egress: Boolean indicating whether packet is from egress or ingress direction from to the pod
 * @arg path:      Pointer to store the matching rule's SFC path key
 *
 * Return `true` if packet matches a traffic selection, `false` if it does not.
 */
static __always_inline __maybe_unused bool
sfc_select(struct __ctx_buff *ctx, struct iphdr *ip4, bool is_egress, struct sfc_path_key *path)
{
	struct sfc_path_key *path_key;
	struct sfc_select_key select_key = {
		.ep_id = LXC_ID,
		.is_egress = is_egress,
		.protocol = ip4->protocol,
	};

	if (is_sfc_encapped(ctx, ip4))
		return false;

	if (ip4->protocol == IPPROTO_UDP || ip4->protocol == IPPROTO_TCP) {
		/* Port offsets for UDP, TCP are the same */
		int off = ETH_HLEN + ipv4_hdrlen(ip4) + TCP_DPORT_OFF;
		int ret = l4_load_port(ctx, off, &select_key.port);
		if (IS_ERR(ret))
			return false;
	} else {
		/* Protocol not supported */
		return false;
	}

	select_key.src_prefix_len = lookup_prefix_len(ip4->saddr, is_egress, false);
	select_key.dst_prefix_len = lookup_prefix_len(ip4->daddr, is_egress, true);
	select_key.src_cidr = mask_ipv4(ip4->saddr, select_key.src_prefix_len),
	select_key.dst_cidr = mask_ipv4(ip4->daddr, select_key.dst_prefix_len),

	path_key = map_lookup_elem(&SFC_SELECT_MAP, &select_key);
	if (path_key == NULL) {
		/* If no match, try "all ports" lookup */
		select_key.port = 0;
		path_key = map_lookup_elem(&SFC_SELECT_MAP, &select_key);
	}
	if (path_key == NULL) {
		return false;
	}
	*path = *path_key;
	return true;
}

static __always_inline int
__flow4_extract_l4_ports(struct __ctx_buff *ctx, struct iphdr *ip4, int l4_off,
			 enum ct_dir dir __maybe_unused,
			 struct sfc_ipv4_flow_key *key)
{
#ifdef ENABLE_IPV4_FRAGMENTS
	return ipv4_handle_fragmentation(
	    ctx, ip4, l4_off, dir, (struct ipv4_frag_l4ports *)&key->sport,
	    NULL);
#else

	/* load sport + dport */
	if (ctx_load_bytes(ctx, l4_off, &key->sport, 4) < 0) {
		return DROP_CT_INVALID_HDR;
	}
#endif
}

static __always_inline bool
__flow4_entry_alive(const struct sfc_ipv4_flow_entry *entry)
{
	return !entry->rx_closing || !entry->tx_closing;
}

/**
 * Update the flow timeouts for the specified entry.
 */
static __always_inline void
__flow4_update_timeout(struct sfc_ipv4_flow_entry *entry, bool tcp, int dir,
		       union tcp_flags tcp_flags)
{
	__u32 lifetime = bpf_sec_to_mono(CT_CONNECTION_LIFETIME_NONTCP);
	bool syn = tcp_flags.value & TCP_FLAG_SYN;
	__u32 now = bpf_mono_now();

	if (tcp) {
		entry->seen_non_syn |= !syn;
		if (entry->seen_non_syn) {
			lifetime = bpf_sec_to_mono(CT_CONNECTION_LIFETIME_TCP);
		} else {
			lifetime = bpf_sec_to_mono(CT_SYN_TIMEOUT);
		}

		if (dir == CT_EGRESS) {
			entry->seen_tx_syn = entry->seen_tx_syn | syn;
		} else {
			entry->seen_rx_syn = entry->seen_rx_syn | syn;
		}

		if (syn) {
			// reopen if needed.
			entry->rx_closing = 0;
			entry->tx_closing = 0;
		} else if ((tcp_flags.value & TCP_FLAG_RST) ||
			   (tcp_flags.value & TCP_FLAG_FIN)) {
			// For incomplete connections (not seen syn both ways),
			// terminate the connection on RST.
			if ((tcp_flags.value & TCP_FLAG_RST) &&
			    !(entry->seen_tx_syn && entry->seen_rx_syn)) {
				entry->rx_closing = 1;
				entry->tx_closing = 1;
			} else if (dir == CT_EGRESS) {
				entry->tx_closing = 1;
			} else {
				entry->rx_closing = 1;
			}
			if (!__flow4_entry_alive(entry)) {
				lifetime = bpf_sec_to_mono(CT_CLOSE_TIMEOUT);
				WRITE_ONCE(entry->lifetime, now + lifetime);
				return;
			}
		}
	}

	// If the entry is not alive, do not refresh lifetime.
	if (__flow4_entry_alive(entry)) {
		WRITE_ONCE(entry->lifetime, now + lifetime);
	}
}

static __always_inline bool __flow4_lookup(const struct sfc_ipv4_flow_key *key,
					   union tcp_flags tcp_flags, int dir,
					   struct sfc_ipv4_flow_entry *entry)
{
	bool is_tcp = (key->nexthdr == IPPROTO_TCP);
	struct sfc_ipv4_flow_entry *f;

	f = map_lookup_elem(&SFC_FLOW_MAP_ANY4, key);
	if (f) {
		__flow4_update_timeout(f, is_tcp, dir, tcp_flags);
		*entry = *f;
		return true;
	}
	return false;
}

static __always_inline __maybe_unused bool
__flow_create4(const struct sfc_ipv4_flow_key *key, union tcp_flags tcp_flags,
	       struct sfc_ipv4_flow_entry *entry)
{
	bool is_tcp = (key->nexthdr == IPPROTO_TCP);
	struct sfc_ipv4_flow_entry new_entry = {
	    .path = entry->path, .previous_hop_addr = entry->previous_hop_addr};

	__flow4_update_timeout(&new_entry, is_tcp, CT_EGRESS, tcp_flags);
	if (map_update_elem(&SFC_FLOW_MAP_ANY4, key, &new_entry, 0) < 0) {
		return false;
	}
	*entry = new_entry;
	return true;
}

/**
 * Lookup service steering flows. Creates the entry if not reverse.
 * @arg ctx:       Packet
 * @arg ip4:       Pointer to L3 header
 * @arg reverse: Boolean indicating whether packet is the return packet from
 * destination.
 * @arg entry:      Pointer to store the matching entry. If creating a new
 * entry, only path and previous_hop_addr in the entry are used.
 *
 * Return negative `DROP_` codes if the packet can't be handled. `CT_NEW` if
 * reverse and no matching entry found. `CT_ESTABLISHED` if an entry is found or
 * created.
 */
static __always_inline __maybe_unused int
sfc_flow_lookup4(struct __ctx_buff *ctx, struct iphdr *ip4, bool reverse,
		 struct sfc_ipv4_flow_entry *entry)
{
	struct sfc_ipv4_flow_key key = {};
	enum ct_dir dir = CT_EGRESS;
	union tcp_flags tcp_flags = {.value = 0};
	__be16 tmp_port = 0;
	int ret;
	int l4_off = ETH_HLEN + ipv4_hdrlen(ip4);

	if (ip4->protocol != IPPROTO_UDP && ip4->protocol != IPPROTO_TCP) {
		/* Protocol not supported */
		return DROP_CT_UNKNOWN_PROTO;
	}

	key.saddr = ip4->saddr;
	key.daddr = ip4->daddr;
	key.nexthdr = ip4->protocol;
	if (reverse) {
		dir = CT_INGRESS;
		key.daddr = ip4->saddr;
		key.saddr = ip4->daddr;
	}

	ret = __flow4_extract_l4_ports(ctx, ip4, l4_off, dir, &key);
	if IS_ERR (ret) {
		return ret;
	}

	if (reverse) {
		tmp_port = key.sport;
		key.sport = key.dport;
		key.dport = tmp_port;
	}
	if (ip4->protocol == IPPROTO_TCP) {
		if (ctx_load_bytes(ctx, l4_off + 12, &tcp_flags, 2) < 0)
			return DROP_CT_INVALID_HDR;
	}

	if (__flow4_lookup(&key, tcp_flags, dir, entry)) {
		return CT_ESTABLISHED;
	}
	if (reverse) {
		return CT_NEW;
	}

	if (!__flow_create4(&key, tcp_flags, entry)) {
		return DROP_CT_CREATE_FAILED;
	}
	return CT_ESTABLISHED;
}

#endif /* ENABLE_GOOGLE_SERVICE_STEERING */

#endif /* __LIB_GOOGLE_SFC_H_ */