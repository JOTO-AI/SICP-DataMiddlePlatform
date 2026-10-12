-- Append-only, transactional migration; deploy to all four slots before new code.
-- Membership batches select JXBH sets and page by source primary key. The older
-- (JXBH,JHKCH,PKH) schedule index has a different ordering and stays in place.
CREATE INDEX dwd_bzkspksjzlb_class_cursor_c ON source.dwd_bzkspksjzlb ("JXBH" COLLATE "C", "PKH" COLLATE "C");
CREATE INDEX dwd_bzkskbsjzlb_class_cursor_c ON source.dwd_bzkskbsjzlb ("JXBH" COLLATE "C", "ID" COLLATE "C");
