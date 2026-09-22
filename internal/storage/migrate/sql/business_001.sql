REVOKE CREATE ON SCHEMA public FROM PUBLIC;
DO $$ BEGIN
    EXECUTE format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_query, sicp_worker', current_database());
END $$;

CREATE SCHEMA source;

REVOKE ALL ON SCHEMA source FROM PUBLIC;

GRANT USAGE ON SCHEMA source TO sicp_query, sicp_worker;

CREATE TABLE source.dwd_bzksbjsjzlb (
    "BH" TEXT PRIMARY KEY CHECK ("BH" <> ''),
    "BJMC" TEXT,
    "BJJC" TEXT,
    "BJLBM" TEXT,
    "SSXQH" TEXT,
    "SSDWH" TEXT,
    "SSNJ" TEXT,
    "JBNY" TEXT,
    "BJRS" TEXT,
    "YXZTM" TEXT,
    "BZRJGH" TEXT,
    "FDYJGH" TEXT,
    "BZXH" TEXT,
    "SSZYH" TEXT,
    "SFDDB" TEXT,
    "NJZYDM" TEXT,
    "XQH" TEXT,
    "XZ" JSON,
    "NJZYMC" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzkscjzlb (
    "ID" TEXT PRIMARY KEY CHECK ("ID" <> ''),
    "XH" TEXT,
    "XN" TEXT,
    "XQM" TEXT,
    "KCH" TEXT,
    "XF" JSON,
    "JD" JSON,
    "KCCJ" JSON,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzksgrtxsjzlb (
    "XH" TEXT PRIMARY KEY CHECK ("XH" <> ''),
    "YDDH" TEXT,
    "DZXX" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzksjbsjzlb (
    "XH" TEXT PRIMARY KEY CHECK ("XH" <> ''),
    "XM" TEXT,
    "YWXM" TEXT,
    "XMPY" TEXT,
    "HYZKM" TEXT,
    "CYM" TEXT,
    "XBM" TEXT,
    "SFDSZN" TEXT,
    "GJDQM" TEXT,
    "JGM" TEXT,
    "CCQJ" TEXT,
    "SFZJYXQ" TEXT,
    "TC" TEXT,
    "AH" TEXT,
    "YHKH" TEXT,
    "CSRQ" TEXT,
    "CSDM" TEXT,
    "SFZJH" TEXT,
    "SFZJLXM" TEXT,
    "GATQWM" TEXT,
    "JKZKM" TEXT,
    "ZZMMM" TEXT,
    "XXM" TEXT,
    "MZM" TEXT,
    "XYZJM" TEXT,
    "RLSPL" TEXT,
    "SYDM" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzkskbsjzlb (
    "ID" TEXT PRIMARY KEY CHECK ("ID" <> ''),
    "XH" TEXT,
    "JHKCH" TEXT,
    "JXBH" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzkskcsjzlb (
    "KCH" TEXT PRIMARY KEY CHECK ("KCH" <> ''),
    "KCMC" TEXT,
    "KCYWMC" TEXT,
    "KCJJ" TEXT,
    "XF" JSON,
    "ZHXS" JSON,
    "ZXS" JSON,
    "KCLBM" TEXT,
    "KCSXM" TEXT,
    "KCXZM" TEXT,
    "SFXQHZKFKC" TEXT,
    "SFKZRTKC" TEXT,
    "KCLXM" TEXT,
    "KKXY" TEXT,
    "SFYX" TEXT,
    "SFWLK" TEXT,
    "KCBQ" TEXT,
    "SFCXKC" TEXT,
    "CXKCH" TEXT,
    "CXKCMC" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzksnjzysjzlb (
    "NJZYDM" TEXT PRIMARY KEY CHECK ("NJZYDM" <> ''),
    "NJZYMC" TEXT,
    "NJ" TEXT,
    "DWH" TEXT,
    "XQH" TEXT,
    "ZYH" TEXT,
    "ZYMC" TEXT,
    "XSLBM" TEXT,
    "PYCCM" TEXT,
    "XZ" JSON,
    "ETL_TIME" TIMESTAMP WITHOUT TIME ZONE
);

CREATE TABLE source.dwd_bzkspksjzlb (
    "JHKCH" TEXT,
    "PKH" TEXT PRIMARY KEY CHECK ("PKH" <> ''),
    "KCXZM" TEXT,
    "SKRQ" TEXT,
    "SKSJ" TEXT,
    "JXZY" TEXT,
    "KRL" JSON,
    "ZC" TEXT,
    "XQJ" TEXT,
    "SKJC" TEXT,
    "JXTZ" TEXT,
    "XKNJ" TEXT,
    "SKBJH" TEXT,
    "SKJSH" TEXT,
    "KKXND" TEXT,
    "KKXQM" TEXT,
    "JSJGH" TEXT,
    "JSXM" TEXT,
    "RKJSM" TEXT,
    "XKRSXD" JSON,
    "XDRS" JSON,
    "JXBH" TEXT,
    "JXBMC" TEXT,
    "QSZ" JSON,
    "ZZZ" JSON,
    "KKSM" TEXT,
    "PKYQ" TEXT,
    "JSSZXQH" TEXT,
    "XKXQH" TEXT,
    "KCKSDWH" TEXT,
    "JXDD" TEXT,
    "JSLXM" TEXT,
    "SKDDLBM" TEXT,
    "XYJCCBH" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzksxjjbsjzlb (
    "XH" TEXT PRIMARY KEY CHECK ("XH" <> ''),
    "SZNJ" TEXT,
    "SZBH" TEXT,
    "HDXLFSM" TEXT,
    "XKMLM" TEXT,
    "RXNY" TEXT,
    "SSXQH" TEXT,
    "XZ" JSON,
    "SFXFZ" TEXT,
    "ZCXDNX" TEXT,
    "YXSH" TEXT,
    "PYFSM" TEXT,
    "YJBYRQ" TEXT,
    "KSH" TEXT,
    "PYCCM" TEXT,
    "XSLBM" TEXT,
    "XSDQZTM" TEXT,
    "XJZTM" TEXT,
    "LDFS" TEXT,
    "ZYH" TEXT,
    "SFQRZ" TEXT,
    "SFZX" TEXT,
    "XXXSM" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_bzkszyxxsjzlb (
    "ZYH" TEXT PRIMARY KEY CHECK ("ZYH" <> ''),
    "ZYJC" TEXT,
    "ZYMC" TEXT,
    "BZKZYM" TEXT,
    "XKMLM" TEXT,
    "SSZYHDL" TEXT,
    "ZYFLM" TEXT,
    "DWH" TEXT,
    "ZYCCM" TEXT,
    "QSXQM" TEXT,
    "ZYYWMC" TEXT,
    "XZ" JSON,
    "JLNY" TEXT,
    "PZSZNY" TEXT,
    "SCZSNY" TEXT,
    "SYXWLBM" TEXT,
    "SSZYQH" TEXT,
    "PYFSM" TEXT,
    "SFSFLZY" TEXT,
    "SFZZZY" TEXT,
    "FZRJGH" TEXT,
    "ZSZTM" TEXT,
    "YXZTM" TEXT,
    "XSLBM" TEXT,
    "PYCCM" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_grtxfszlb (
    "JGH" TEXT PRIMARY KEY CHECK ("JGH" <> ''),
    "YDDH" TEXT,
    "DZXX" TEXT,
    "ETL_TIME" TEXT
);

CREATE TABLE source.dwd_jzgjcsjzlb (
    "JGH" TEXT PRIMARY KEY CHECK ("JGH" <> ''),
    "DWH" TEXT,
    "XM" TEXT,
    "YWXM" TEXT,
    "XMPY" TEXT,
    "CYM" TEXT,
    "XBM" TEXT,
    "CSRQ" TEXT,
    "CSDM" TEXT,
    "MZM" TEXT,
    "GJDQM" TEXT,
    "SFZJLXM" TEXT,
    "SFZJH" TEXT,
    "HYZKM" TEXT,
    "GATQWM" TEXT,
    "ZZMMM" TEXT,
    "JKZKM" TEXT,
    "XYZJM" TEXT,
    "XXM" TEXT,
    "SFZJYXQ" TEXT,
    "XQH" TEXT,
    "ZGXLM" TEXT,
    "WHCDM" TEXT,
    "CJGZNY" TEXT,
    "LXRQ" TEXT,
    "QXRQ" TEXT,
    "CJNY" TEXT,
    "BZLBM" TEXT,
    "JZGLBM" TEXT,
    "RKZKM" TEXT,
    "DABH" TEXT,
    "DAWB" TEXT,
    "DQZTM" TEXT,
    "TC" TEXT,
    "XKLBM" TEXT,
    "YJXKM" TEXT,
    "EJXKM" TEXT,
    "YJFX" TEXT,
    "ZGZCM" TEXT,
    "ZGXWM" TEXT,
    "GWMCM" TEXT,
    "GWDJM" TEXT,
    "GCCRCLXM" TEXT,
    "HTQDQKM" TEXT,
    "ZGZYZG" TEXT,
    "SFSJT" TEXT,
    "SFSSX" TEXT,
    "XYJGM" TEXT,
    "XZSSZYH" TEXT,
    "JGM" TEXT,
    "YRFSM" TEXT,
    "YHKH" TEXT,
    "SFZB" TEXT,
    "SFSFS" TEXT,
    "WPJS" TEXT,
    "JZBM" TEXT,
    "ETL_TIME" TIMESTAMP WITHOUT TIME ZONE
);

CREATE TABLE source.dwd_xqjbsjzlb (
    "XQH" TEXT PRIMARY KEY CHECK ("XQH" <> ''),
    "XQMC" TEXT,
    "XQCX" TEXT,
    "ETL_TIME" TIMESTAMP WITHOUT TIME ZONE
);

CREATE TABLE source.dwd_yxsdwjbsjzlb (
    "DWH" TEXT PRIMARY KEY CHECK ("DWH" <> ''),
    "DWMC" TEXT,
    "DWYWMC" TEXT,
    "DWJC" TEXT,
    "DWYWJC" TEXT,
    "DWJP" TEXT,
    "DWDZ" TEXT,
    "LSDWH" TEXT,
    "DWLBM" TEXT,
    "DWBBM" TEXT,
    "DWYXBSM" TEXT,
    "SXRQ" TEXT,
    "SFST" TEXT,
    "JLNY" TEXT,
    "DWFZRJGH" TEXT,
    "SFGK" TEXT,
    "LSDZZBH" TEXT,
    "DZZFZRJGH" TEXT,
    "LSXQH" TEXT,
    "SFSC" TEXT,
    "FGXLD" TEXT,
    "JLRQ" TEXT,
    "CJR" TEXT,
    "ZJXGRJGH" TEXT,
    "ZHXGSJ" TEXT,
    "ETL_TIME" TEXT
);

GRANT SELECT ON ALL TABLES IN SCHEMA source TO sicp_query;

GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA source TO sicp_worker;

ALTER DEFAULT PRIVILEGES IN SCHEMA source GRANT SELECT ON TABLES TO sicp_query;

ALTER DEFAULT PRIVILEGES IN SCHEMA source GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON TABLES TO sicp_worker;
