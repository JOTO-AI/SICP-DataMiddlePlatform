-- Append-only transaction migration. Apply in a controlled deployment window.
-- Keep the original primary constraints and their default collation unchanged.
CREATE INDEX dwd_bzksbjsjzlb_cursor_c ON source.dwd_bzksbjsjzlb ("BH" COLLATE "C");
CREATE INDEX dwd_bzkscjzlb_cursor_c ON source.dwd_bzkscjzlb ("ID" COLLATE "C");
CREATE INDEX dwd_bzksgrtxsjzlb_cursor_c ON source.dwd_bzksgrtxsjzlb ("XH" COLLATE "C");
CREATE INDEX dwd_bzksjbsjzlb_cursor_c ON source.dwd_bzksjbsjzlb ("XH" COLLATE "C");
CREATE INDEX dwd_bzkskbsjzlb_cursor_c ON source.dwd_bzkskbsjzlb ("ID" COLLATE "C");
CREATE INDEX dwd_bzkskcsjzlb_cursor_c ON source.dwd_bzkskcsjzlb ("KCH" COLLATE "C");
CREATE INDEX dwd_bzksnjzysjzlb_cursor_c ON source.dwd_bzksnjzysjzlb ("NJZYDM" COLLATE "C");
CREATE INDEX dwd_bzkspksjzlb_cursor_c ON source.dwd_bzkspksjzlb ("PKH" COLLATE "C");
CREATE INDEX dwd_bzksxjjbsjzlb_cursor_c ON source.dwd_bzksxjjbsjzlb ("XH" COLLATE "C");
CREATE INDEX dwd_bzkszyxxsjzlb_cursor_c ON source.dwd_bzkszyxxsjzlb ("ZYH" COLLATE "C");
CREATE INDEX dwd_grtxfszlb_cursor_c ON source.dwd_grtxfszlb ("JGH" COLLATE "C");
CREATE INDEX dwd_jzgjcsjzlb_cursor_c ON source.dwd_jzgjcsjzlb ("JGH" COLLATE "C");
CREATE INDEX dwd_xqjbsjzlb_cursor_c ON source.dwd_xqjbsjzlb ("XQH" COLLATE "C");
CREATE INDEX dwd_yxsdwjbsjzlb_cursor_c ON source.dwd_yxsdwjbsjzlb ("DWH" COLLATE "C");
CREATE INDEX dwd_bzkscjzlb_student_c ON source.dwd_bzkscjzlb ("XH" COLLATE "C", "ID" COLLATE "C");
CREATE INDEX dwd_bzkskbsjzlb_student_c ON source.dwd_bzkskbsjzlb ("XH" COLLATE "C", "ID" COLLATE "C");
CREATE INDEX dwd_bzkspksjzlb_class_course_c ON source.dwd_bzkspksjzlb ("JXBH" COLLATE "C", "JHKCH" COLLATE "C", "PKH" COLLATE "C");
CREATE INDEX dwd_bzkspksjzlb_term_c ON source.dwd_bzkspksjzlb ("KKXND" COLLATE "C", "KKXQM" COLLATE "C", "PKH" COLLATE "C");
-- Required for post-import ANALYZE on PostgreSQL 18; query role is unchanged.
GRANT MAINTAIN ON ALL TABLES IN SCHEMA source TO sicp_worker;
