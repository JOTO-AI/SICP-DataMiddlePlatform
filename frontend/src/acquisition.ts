export interface TableAcquisition { table: string; status: 'waiting' | 'acquiring' | 'collected'; completed_pages: number; total_pages: number | null }
export interface Acquisition { tables: TableAcquisition[]; updated_at: string }
