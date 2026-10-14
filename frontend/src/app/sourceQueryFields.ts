// Generated from latest_api_docs section 3; regenerate with scripts/generate-source-query-fields.mjs.
export interface SourceQueryField { name: string; type: string; required: string; defaultText: string; description: string }
export const sourceQueryFields: Record<string, SourceQueryField[]> = {
  "dwd_grtxfszlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "JGH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "教工号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_jzgjcsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "JGH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "教工号"
    },
    {
      "name": "DWH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位号"
    },
    {
      "name": "XM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "姓名"
    },
    {
      "name": "SFZJH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "身份证件号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzkszyxxsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzksgrtxsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "XH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzksjbsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "XH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学号"
    },
    {
      "name": "XM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "姓名"
    },
    {
      "name": "XBM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "性别码"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzksxjjbsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "XH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学号"
    },
    {
      "name": "SZNJ",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "所在年级"
    },
    {
      "name": "SZBH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "所在班号"
    },
    {
      "name": "YXSH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "院系所号"
    },
    {
      "name": "XSDQZTM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学生当前状态码"
    },
    {
      "name": "ZYH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "专业号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzksnjzysjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzkscjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "XH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学号"
    },
    {
      "name": "XN",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学年（度）"
    },
    {
      "name": "XQM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学期码"
    },
    {
      "name": "KCH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "课程号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzkspksjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "JHKCH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "计划课程号"
    },
    {
      "name": "PKH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排课号"
    },
    {
      "name": "KKXND",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "开课学年度"
    },
    {
      "name": "KKXQM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "开课学期码"
    },
    {
      "name": "JSJGH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "教师教工号"
    },
    {
      "name": "JXBH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "教学班号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzksbjsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "BH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "班号"
    },
    {
      "name": "BJMC",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "班级名称"
    },
    {
      "name": "SSNJ",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "所属年级"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzkskcsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "KCH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "课程号"
    },
    {
      "name": "KCMC",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "课程名称"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_bzkskbsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "XH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "学号"
    },
    {
      "name": "JHKCH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "计划课程号"
    },
    {
      "name": "JXBH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "教学班号"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_xqjbsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ],
  "dwd_yxsdwjbsjzlb": [
    {
      "name": "access_token",
      "type": "String",
      "required": "是",
      "defaultText": "未提供",
      "description": "由服务端部署配置；浏览器不读取或编辑令牌。"
    },
    {
      "name": "page",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "当前页"
    },
    {
      "name": "pageSize",
      "type": "Long",
      "required": "是",
      "defaultText": "未提供",
      "description": "分页大小"
    },
    {
      "name": "sort",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "排序，传该参数会覆盖默认的按主键升序排序"
    },
    {
      "name": "DWH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位号"
    },
    {
      "name": "DWMC",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位名称"
    },
    {
      "name": "LSDWH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "隶属单位号"
    },
    {
      "name": "DWLBM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位类别码"
    },
    {
      "name": "DWYXBSM",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位有效标识码"
    },
    {
      "name": "DWFZRJGH",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "单位负责人教工号"
    },
    {
      "name": "SFSC",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "是否删除"
    },
    {
      "name": "FGXLD",
      "type": "String",
      "required": "否",
      "defaultText": "未提供",
      "description": "分管校领导"
    },
    {
      "name": "startTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "开始时间，过滤数仓表ETL_TIME字段"
    },
    {
      "name": "endTime",
      "type": "LocalDateTime",
      "required": "否",
      "defaultText": "未提供",
      "description": "结束时间，过滤数仓表ETL_TIME字段"
    }
  ]
}
