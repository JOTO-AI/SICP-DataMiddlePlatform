export interface TableAcquisition { table: string; status: 'waiting' | 'checking' | 'reusing' | 'acquiring' | 'collected' | 'failed'; completed_pages: number; total_pages: number | null; reused?: boolean }
export interface Acquisition { tables: TableAcquisition[]; updated_at: string }
export const sourceTables = ['dwd_bzksbjsjzlb', 'dwd_bzkscjzlb', 'dwd_bzksgrtxsjzlb', 'dwd_bzksjbsjzlb', 'dwd_bzkskbsjzlb', 'dwd_bzkskcsjzlb', 'dwd_bzksnjzysjzlb', 'dwd_bzkspksjzlb', 'dwd_bzksxjjbsjzlb', 'dwd_bzkszyxxsjzlb', 'dwd_grtxfszlb', 'dwd_jzgjcsjzlb', 'dwd_xqjbsjzlb', 'dwd_yxsdwjbsjzlb']
