// Display names reviewed against latest_api_docs headings; IDs and backend rows stay unchanged.
export const sourceNames: Record<string, string> = {
  dwd_jzgjcsjzlb: '教职工基础', dwd_grtxfszlb: '教职工通讯',
  dwd_bzksjbsjzlb: '学生基本信息', dwd_bzksgrtxsjzlb: '学生通讯',
  dwd_bzksxjjbsjzlb: '学生学籍', dwd_bzkszyxxsjzlb: '专业',
  dwd_bzksnjzysjzlb: '年级专业', dwd_bzksbjsjzlb: '班级',
  dwd_bzkskcsjzlb: '课程', dwd_bzkscjzlb: '成绩', dwd_bzkspksjzlb: '排课',
  dwd_bzkskbsjzlb: '学生课表', dwd_xqjbsjzlb: '校区', dwd_yxsdwjbsjzlb: '院系所单位',
}
export const sourceName = (id: string): string => sourceNames[id] ?? id
