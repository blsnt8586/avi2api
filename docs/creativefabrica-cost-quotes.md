# Creative Fabrica 官方报价与本地计费

## 查询入口

管理后台「模型与计价」选择 Creative Fabrica，展开「官方即时报价」。

1. 选择账号和模型。
2. 填写时长、分辨率、质量等模型参数。
3. 对有素材计费条件的模型，填写 `input_media` 元数据。
4. 点击「查询官方 Coins」。这不会生成内容、预留积分或替换本地价格规则。
5. 「已核对的参数报价与默认整单价」展示审计时精确参数对应的官方响应。

参数定义和报价表是带日期的快照。即时报价会重新获取账号目录并调用官方
`CalculateModelCost`，不从另一平台套用公式。

## 素材计费元数据

```json
[
  {
    "role": "referenceVideo",
    "metadata": {
      "durationSeconds": 4
    }
  }
]
```

具体角色和必填元数据由该模型的 `pricingInputs.media` 声明。源视频工作流还可能
需要 `widthPx` 和 `heightPx`；源音频工作流可能需要 `sourceAudio`。
多个素材分别列出，不把输入时长和输出时长混为一谈。

官方计算器的元数据字段接受整数；当前官网
`build-pricing-input-media-CQ2YDSj8.js` 对正数调用 `Math.round`。
入口对源视频、参考视频复用同一四舍五入规则（4.25 → 4，4.5 → 5），并在响应中保留实际提交的元数据。
独立音频工作流的小数秒换算尚未核实，目前只接受整数元数据，不外推视频规则。

## 管理端接口

`POST /admin/api/accounts/{id}/cost-quote` 使用管理员会话及 CSRF 校验，
不是使用对外 API Key 的生成接口。

```json
{
  "kind": "video",
  "model": "creativefabrica/seedance_v2_5",
  "options": {
    "duration_seconds": 5,
    "resolution": "720p",
    "aspect_ratio": "16:9"
  },
  "input_media": [
    {"role": "referenceVideo", "metadata": {"durationSeconds": 4}}
  ]
}
```

响应包括 `coins`、`pricing_type`、有效请求参数、`resolved_options`、
`checked_at` 和 `installed=false`。输入使用普通 JSON 标量；
服务端按模型类型编码 Connect oneof，并仅补充上游明确声明的默认值。
角色、模型选项和选项范围按该账号当前目录校验。

无参考图片还查询官网的 `FlowService.CalculateGenerationCost`，返回
`default_flow_total` 和 `default_flow_quantity`，明确标记为默认 Flow 设置。
它与 `AIProviderService.CalculateModelCost` 是不同的报价路径，不得混用。
本次观测 Nano Banana 2 通用计算器报价 3,750 Coins，默认 Flow 单张 3,500 Coins；
当前图片任务适配器实际调用 `FlowService.CreateFlow`。
默认 Flow 的 1～20 张精确报价快照也在面板内提供，但不表示网关生成接口支持 20 张。

## 和自动计费的边界

- 官方报价成功不证明实际生成权限，也不是已经扣费的账单。
- 静态模型返回的模型报价，不自动解释为任意输出张数的整单金额。优先使用对应
  生成链路的整单计算器；两条报价路径在本次实测中确实存在差异。
- 本地已启用的不可变价格规则仍用于任务预留与结算；即时报价不自动安装规则。
- 素材敏感报价、批量输出整单费用和实际生成参数映射仍须完成
  接线及对账，才可以宣称完整自动计费。不能把无参考报价用作参考工作流报价。
- 同步时，不同上游参数全部保留用于报价；若本地价格维度容纳不了两个不同报价，
  同步明确报错，而不是静默选用第一条。超过同步组合上限也会明确失败，不截断快照。
- 此次目录审计包含原始上游型号，不改变「仅开放 2026 年发布版本」的独立筛选要求。
