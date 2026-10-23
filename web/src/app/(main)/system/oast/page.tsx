"use client";

import * as React from "react";
import { Loader2Icon, RadarIcon, RefreshCwIcon, SaveIcon, SendIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api, type OASTStatus, type OASTToken } from "@/lib/api";

function fmtTime(ts: string) {
  if (!ts) return "-";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleTimeString("zh-CN");
}

// 带外通道（OAST）—— 盲 SSRF / 盲命令注入 / 盲 XXE 的**唯一判据**。
//
// 有些漏洞目标不回显任何东西。你要判断"它到底执行了没有"，只能给它一个你能收到信的地址：
// 目标一旦真去访问，这里就会记下一条（带源 IP 和时间）。收到 = 漏洞成立；没收到 = 不成立。
//
// 为什么要填"外部可达地址"：监听地址（0.0.0.0:8888）是给自己听的，目标访问不到。
// Kali 虚拟机是 NAT，外网进不来 —— 所以现在留空即可（留空=整条链不启动，零开销）。
// 等有公网 VPS，把监听打开、再把外部地址填成 http://你的IP:端口，粘上就生效。
export default function OastPage() {
  const [status, setStatus] = React.useState<OASTStatus | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [httpAddr, setHttpAddr] = React.useState("");
  const [publicBase, setPublicBase] = React.useState("");
  const [token, setToken] = React.useState<OASTToken | null>(null);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.oastStatus();
      setStatus(res);
      setHttpAddr(res.http_addr ?? "");
      setPublicBase(res.public_base ?? "");
    } catch (e) {
      toast.error("加载失败：" + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    try {
      const res = await api.saveOAST(httpAddr, publicBase);
      toast.success(res.enabled ? "已保存并启动通道" : "已保存（未填监听地址，通道关闭）");
      await load();
    } catch (e) {
      toast.error("保存失败：" + (e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    try {
      const tok = await api.oastTest();
      setToken(tok);
      toast.success("已生成探测地址，用浏览器或 curl 访问它，下面就会出现一条记录");
    } catch (e) {
      toast.error("生成失败：" + (e as Error).message);
    }
  };

  const hits = status?.hits ?? [];

  return (
    <div className="flex flex-col gap-4 p-6">
      <div>
        <h1 className="flex items-center gap-2 text-xl font-semibold">
          <RadarIcon className="size-5" /> 带外通道（OAST）
        </h1>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
          用来验证<b>看不见的漏洞</b>：盲 SSRF、盲命令注入、盲 XXE、JNDI 注入。目标不回显任何东西时，
          唯一判据是「它有没有真的访问你给的地址」—— 这里记到一条，就是漏洞成立，还带着源 IP 和时间。<br />
          <span className="text-xs">
            本机（Kali/NAT）留空即可：留空 = 通道不启动，零开销、不影响其它功能。等有公网 VPS，
            打开监听地址、把外部可达地址填上去，就生效。
          </span>
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">配置</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="grid gap-3 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium">监听地址（空 = 关闭通道）</label>
              <Input placeholder=":8888" value={httpAddr} onChange={(e) => setHttpAddr(e.target.value)} />
              <p className="mt-1 text-[11px] text-muted-foreground">本机随便一个空闲端口即可，例 <code>:8888</code></p>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium">外部可达基址（VPS 时才填）</label>
              <Input
                placeholder="http://1.2.3.4:8888 或 https://oast.你的域名"
                value={publicBase}
                onChange={(e) => setPublicBase(e.target.value)}
              />
              <p className="mt-1 text-[11px] text-muted-foreground">
                目标机要能访问到这个地址；不填的话生成的地址只在本机通
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={() => void save()} disabled={saving} size="sm">
              {saving ? <Loader2Icon className="size-4 animate-spin" /> : <SaveIcon className="size-4" />} 保存并应用
            </Button>
            <Button variant="outline" size="sm" onClick={() => void test()} disabled={!status?.enabled}>
              <SendIcon className="size-4" /> 生成测试地址
            </Button>
            <Button variant="ghost" size="sm" onClick={() => void load()} disabled={loading}>
              <RefreshCwIcon className={loading ? "size-4 animate-spin" : "size-4"} /> 刷新
            </Button>
            <Badge variant={status?.enabled ? "default" : "secondary"} className="ml-auto">
              {status?.enabled ? `运行中 ${status.listen_addr ?? ""}` : "未启动"}
            </Badge>
          </div>

          {token && (
            <div className="rounded-md border bg-muted/40 p-3 text-xs">
              <div className="mb-1 font-medium">刚生成的探测地址（去访问它，然后回来看记录）</div>
              {token.http_url && (
                <div className="font-mono break-all">
                  HTTP: <a className="underline" href={token.http_url} target="_blank" rel="noreferrer">{token.http_url}</a>
                </div>
              )}
              {token.dns_host && <div className="font-mono break-all">DNS: {token.dns_host}</div>}
              <div className="mt-1 text-muted-foreground">
                命令行验证：<code>curl &quot;{token.http_url ?? ""}&quot;</code>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="overflow-hidden">
        <CardHeader>
          <CardTitle className="text-base">回调记录（{hits.length}）</CardTitle>
        </CardHeader>
        {hits.length === 0 ? (
          <CardContent className="text-sm text-muted-foreground">
            还没有回连记录。用「生成测试地址」并访问它，这里就会出现第一条。
          </CardContent>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[110px]">时间</TableHead>
                <TableHead className="w-[70px]">协议</TableHead>
                <TableHead className="w-[140px]">来源 IP</TableHead>
                <TableHead>详情</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {hits.map((h, i) => (
                <TableRow key={`${h.token_id}-${i}`}>
                  <TableCell className="whitespace-nowrap text-xs">{fmtTime(h.at)}</TableCell>
                  <TableCell className="text-xs uppercase">{h.proto}</TableCell>
                  <TableCell className="font-mono text-xs">{h.source}</TableCell>
                  <TableCell className="max-w-[520px] truncate font-mono text-[11px]" title={h.detail}>
                    {h.detail}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}
