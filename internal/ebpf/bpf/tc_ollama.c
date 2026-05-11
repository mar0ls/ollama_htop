// TC ingress program — captures TCP payloads from Ollama (src_port 11434).
//
// Works on both loopback (lo, ifindex 1) and real Ethernet interfaces (eth0/ens*).
// On loopback, skb->data points directly at the IP header — the kernel pulls the
// pseudo-Ethernet frame in loopback_xmit() before TC ingress runs.
// On real Ethernet interfaces, skb->data includes the 14-byte Ethernet header.
// We distinguish the two by checking skb->ingress_ifindex.
//
// Requires: kernel 6.6+ (TCX API), CAP_NET_ADMIN.

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/tcp.h>
#include <linux/ip.h>
#include <linux/in.h>
#include <linux/if_ether.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define OLLAMA_PORT  11434
#define MAX_PAYLOAD  4095

struct tcp_event {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 seq;
    __u8  fin;
    __u8  _pad[3];
    __u16 data_len;
    __u8  data[MAX_PAYLOAD + 1]; // +1 so array size == 4096
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24); // 16 MB
} events SEC(".maps");

SEC("tc")
int capture_ollama(struct __sk_buff *skb)
{
    /* ── Only handle IPv4 ──────────────────────────────────────────────
       skb->protocol is set before TC ingress runs and is reliable on
       all interface types.                                              */
    if (skb->protocol != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    /* ── Determine L3 offset ────────────────────────────────────────────
       loopback (ifindex 1): no Ethernet header, data IS the IP header.
       Ethernet interfaces:  data has a 14-byte Ethernet header.
       skb->ingress_ifindex == skb->dev->ifindex, set by cls_bpf before
       the program runs.                                                 */
    __u32 nhoff = (skb->ingress_ifindex == 1) ? 0 : ETH_HLEN;

    /* ── IPv4 ───────────────────────────────────────────────────────── */
    struct iphdr *ip = data + nhoff;
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;

    __u32 ip_hlen = ip->ihl * 4;
    if (ip_hlen < 20)
        return TC_ACT_OK;

    /* ── TCP ────────────────────────────────────────────────────────── */
    struct tcphdr *tcp = (void *)ip + ip_hlen;
    if ((void *)(tcp + 1) > data_end)
        return TC_ACT_OK;

    __u16 sport = bpf_ntohs(tcp->source);
    if (sport != OLLAMA_PORT)
        return TC_ACT_OK;

    __u16 dport   = bpf_ntohs(tcp->dest);
    __u32 pay_off = nhoff + ip_hlen + (tcp->doff * 4);

    /* ── FIN with no payload — emit zero-data event ─────────────────── */
    if (pay_off >= skb->len) {
        if (!tcp->fin)
            return TC_ACT_OK;
        struct tcp_event *fin_ev = bpf_ringbuf_reserve(&events, sizeof(*fin_ev), 0);
        if (!fin_ev)
            return TC_ACT_OK;
        fin_ev->src_ip   = ip->saddr;
        fin_ev->dst_ip   = ip->daddr;
        fin_ev->src_port = sport;
        fin_ev->dst_port = dport;
        fin_ev->seq      = bpf_ntohl(tcp->seq);
        fin_ev->fin      = 1;
        fin_ev->data_len = 0;
        bpf_ringbuf_submit(fin_ev, 0);
        return TC_ACT_OK;
    }

    /* ── Data path ──────────────────────────────────────────────────── */
    __u32 raw     = skb->len - pay_off;
    __u32 to_copy = raw;
    if (to_copy > MAX_PAYLOAD)
        to_copy = MAX_PAYLOAD;

    if (to_copy == 0)
        return TC_ACT_OK;

    struct tcp_event *ev = bpf_ringbuf_reserve(&events, sizeof(*ev), 0);
    if (!ev)
        return TC_ACT_OK;

    ev->src_ip   = ip->saddr;
    ev->dst_ip   = ip->daddr;
    ev->src_port = sport;
    ev->dst_port = dport;
    ev->seq      = bpf_ntohl(tcp->seq);
    ev->fin      = tcp->fin ? 1 : 0;
    ev->data_len = (__u16)to_copy;

    if (bpf_skb_load_bytes(skb, pay_off, ev->data, to_copy) != 0) {
        bpf_ringbuf_discard(ev, 0);
        return TC_ACT_OK;
    }

    bpf_ringbuf_submit(ev, 0);
    return TC_ACT_OK;
}

char LICENSE[] SEC("license") = "GPL";
