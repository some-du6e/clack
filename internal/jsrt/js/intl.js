// intl.js: ECMA-402 Intl for QuickJS-NG, which has none. Plain JS on top of
// CLDR data (extracted from ICU 78, see the generated block below) so Taut
// plugins and Slack's own code format dates, numbers, lists and relative
// times exactly like Chrome. Evaluated after env.js / web.js, before dom.js.
//
// Provides Intl.{getCanonicalLocales, supportedValuesOf, Locale,
// DateTimeFormat, NumberFormat, PluralRules, RelativeTimeFormat, ListFormat,
// Collator, Segmenter, DisplayNames, DurationFormat} and the locale-aware
// Date/Number/BigInt/String/Array.prototype.toLocale* methods.
//
// Locale data: en (US), en-GB, en-CA, en-AU, en-IN, de, fr, es, it, pt (BR),
// nl, sv, pl, ja, zh (Hans), ko, ru. Other tags fall back to their base
// language, then to navigator.language, then en-US.
// Time zone offsets come from the host (__jr.host.tz, Go's tzdata).
(() => {
  'use strict';
  const G = globalThis;
  const jr = G.__jr;
  const H = jr.host;
  const def = jr.def;
  const ObjectCreate = Object.create, freeze = Object.freeze;
  const hasOwn = (o, k) => Object.prototype.hasOwnProperty.call(o, k);

  // Unicode-property regexes are built on first use: QuickJS compiles regex
  // literals at parse time, which would put their big tables in every boot
  const U = (src) => new RegExp(src, 'u');
  const rangeError = (m) => new RangeError(m);
  const typeError = (m) => new TypeError(m);

  // ---- generated: CLDR data via ICU 78 (Unicode License), see the header ----
  const ZONES = "Africa/Abidjan Africa/Accra Africa/Addis_Ababa Africa/Algiers Africa/Asmera Africa/Bamako Africa/Bangui Africa/Banjul Africa/Bissau Africa/Blantyre Africa/Brazzaville Africa/Bujumbura Africa/Cairo Africa/Casablanca Africa/Ceuta Africa/Conakry Africa/Dakar Africa/Dar_es_Salaam Africa/Djibouti Africa/Douala Africa/El_Aaiun Africa/Freetown Africa/Gaborone Africa/Harare Africa/Johannesburg Africa/Juba Africa/Kampala Africa/Khartoum Africa/Kigali Africa/Kinshasa Africa/Lagos Africa/Libreville Africa/Lome Africa/Luanda Africa/Lubumbashi Africa/Lusaka Africa/Malabo Africa/Maputo Africa/Maseru Africa/Mbabane Africa/Mogadishu Africa/Monrovia Africa/Nairobi Africa/Ndjamena Africa/Niamey Africa/Nouakchott Africa/Ouagadougou Africa/Porto-Novo Africa/Sao_Tome Africa/Tripoli Africa/Tunis Africa/Windhoek America/Adak America/Anchorage America/Anguilla America/Antigua America/Araguaina America/Argentina/La_Rioja America/Argentina/Rio_Gallegos America/Argentina/Salta America/Argentina/San_Juan America/Argentina/San_Luis America/Argentina/Tucuman America/Argentina/Ushuaia America/Aruba America/Asuncion America/Bahia America/Bahia_Banderas America/Barbados America/Belem America/Belize America/Blanc-Sablon America/Boa_Vista America/Bogota America/Boise America/Buenos_Aires America/Cambridge_Bay America/Campo_Grande America/Cancun America/Caracas America/Catamarca America/Cayenne America/Cayman America/Chicago America/Chihuahua America/Ciudad_Juarez America/Coral_Harbour America/Cordoba America/Costa_Rica America/Coyhaique America/Creston America/Cuiaba America/Curacao America/Danmarkshavn America/Dawson America/Dawson_Creek America/Denver America/Detroit America/Dominica America/Edmonton America/Eirunepe America/El_Salvador America/Fort_Nelson America/Fortaleza America/Glace_Bay America/Godthab America/Goose_Bay America/Grand_Turk America/Grenada America/Guadeloupe America/Guatemala America/Guayaquil America/Guyana America/Halifax America/Havana America/Hermosillo America/Indiana/Knox America/Indiana/Marengo America/Indiana/Petersburg America/Indiana/Tell_City America/Indiana/Vevay America/Indiana/Vincennes America/Indiana/Winamac America/Indianapolis America/Inuvik America/Iqaluit America/Jamaica America/Jujuy America/Juneau America/Kentucky/Monticello America/Kralendijk America/La_Paz America/Lima America/Los_Angeles America/Louisville America/Lower_Princes America/Maceio America/Managua America/Manaus America/Marigot America/Martinique America/Matamoros America/Mazatlan America/Mendoza America/Menominee America/Merida America/Metlakatla America/Mexico_City America/Miquelon America/Moncton America/Monterrey America/Montevideo America/Montserrat America/Nassau America/New_York America/Nome America/Noronha America/North_Dakota/Beulah America/North_Dakota/Center America/North_Dakota/New_Salem America/Ojinaga America/Panama America/Paramaribo America/Phoenix America/Port-au-Prince America/Port_of_Spain America/Porto_Velho America/Puerto_Rico America/Punta_Arenas America/Rankin_Inlet America/Recife America/Regina America/Resolute America/Rio_Branco America/Santarem America/Santiago America/Santo_Domingo America/Sao_Paulo America/Scoresbysund America/Sitka America/St_Barthelemy America/St_Johns America/St_Kitts America/St_Lucia America/St_Thomas America/St_Vincent America/Swift_Current America/Tegucigalpa America/Thule America/Tijuana America/Toronto America/Tortola America/Vancouver America/Whitehorse America/Winnipeg America/Yakutat Antarctica/Casey Antarctica/Davis Antarctica/DumontDUrville Antarctica/Macquarie Antarctica/Mawson Antarctica/McMurdo Antarctica/Palmer Antarctica/Rothera Antarctica/Syowa Antarctica/Troll Antarctica/Vostok Arctic/Longyearbyen Asia/Aden Asia/Almaty Asia/Amman Asia/Anadyr Asia/Aqtau Asia/Aqtobe Asia/Ashgabat Asia/Atyrau Asia/Baghdad Asia/Bahrain Asia/Baku Asia/Bangkok Asia/Barnaul Asia/Beirut Asia/Bishkek Asia/Brunei Asia/Calcutta Asia/Chita Asia/Colombo Asia/Damascus Asia/Dhaka Asia/Dili Asia/Dubai Asia/Dushanbe Asia/Famagusta Asia/Gaza Asia/Hebron Asia/Hong_Kong Asia/Hovd Asia/Irkutsk Asia/Jakarta Asia/Jayapura Asia/Jerusalem Asia/Kabul Asia/Kamchatka Asia/Karachi Asia/Katmandu Asia/Khandyga Asia/Krasnoyarsk Asia/Kuala_Lumpur Asia/Kuching Asia/Kuwait Asia/Macau Asia/Magadan Asia/Makassar Asia/Manila Asia/Muscat Asia/Nicosia Asia/Novokuznetsk Asia/Novosibirsk Asia/Omsk Asia/Oral Asia/Phnom_Penh Asia/Pontianak Asia/Pyongyang Asia/Qatar Asia/Qostanay Asia/Qyzylorda Asia/Rangoon Asia/Riyadh Asia/Saigon Asia/Sakhalin Asia/Samarkand Asia/Seoul Asia/Shanghai Asia/Singapore Asia/Srednekolymsk Asia/Taipei Asia/Tashkent Asia/Tbilisi Asia/Tehran Asia/Thimphu Asia/Tokyo Asia/Tomsk Asia/Ulaanbaatar Asia/Urumqi Asia/Ust-Nera Asia/Vientiane Asia/Vladivostok Asia/Yakutsk Asia/Yekaterinburg Asia/Yerevan Atlantic/Azores Atlantic/Bermuda Atlantic/Canary Atlantic/Cape_Verde Atlantic/Faeroe Atlantic/Madeira Atlantic/Reykjavik Atlantic/South_Georgia Atlantic/St_Helena Atlantic/Stanley Australia/Adelaide Australia/Brisbane Australia/Broken_Hill Australia/Darwin Australia/Eucla Australia/Hobart Australia/Lindeman Australia/Lord_Howe Australia/Melbourne Australia/Perth Australia/Sydney Europe/Amsterdam Europe/Andorra Europe/Astrakhan Europe/Athens Europe/Belgrade Europe/Berlin Europe/Bratislava Europe/Brussels Europe/Bucharest Europe/Budapest Europe/Busingen Europe/Chisinau Europe/Copenhagen Europe/Dublin Europe/Gibraltar Europe/Guernsey Europe/Helsinki Europe/Isle_of_Man Europe/Istanbul Europe/Jersey Europe/Kaliningrad Europe/Kiev Europe/Kirov Europe/Lisbon Europe/Ljubljana Europe/London Europe/Luxembourg Europe/Madrid Europe/Malta Europe/Mariehamn Europe/Minsk Europe/Monaco Europe/Moscow Europe/Oslo Europe/Paris Europe/Podgorica Europe/Prague Europe/Riga Europe/Rome Europe/Samara Europe/San_Marino Europe/Sarajevo Europe/Saratov Europe/Simferopol Europe/Skopje Europe/Sofia Europe/Stockholm Europe/Tallinn Europe/Tirane Europe/Ulyanovsk Europe/Vaduz Europe/Vatican Europe/Vienna Europe/Vilnius Europe/Volgograd Europe/Warsaw Europe/Zagreb Europe/Zurich Indian/Antananarivo Indian/Chagos Indian/Christmas Indian/Cocos Indian/Comoro Indian/Kerguelen Indian/Mahe Indian/Maldives Indian/Mauritius Indian/Mayotte Indian/Reunion Pacific/Apia Pacific/Auckland Pacific/Bougainville Pacific/Chatham Pacific/Easter Pacific/Efate Pacific/Enderbury Pacific/Fakaofo Pacific/Fiji Pacific/Funafuti Pacific/Galapagos Pacific/Gambier Pacific/Guadalcanal Pacific/Guam Pacific/Honolulu Pacific/Kiritimati Pacific/Kosrae Pacific/Kwajalein Pacific/Majuro Pacific/Marquesas Pacific/Midway Pacific/Nauru Pacific/Niue Pacific/Norfolk Pacific/Noumea Pacific/Pago_Pago Pacific/Palau Pacific/Pitcairn Pacific/Ponape Pacific/Port_Moresby Pacific/Rarotonga Pacific/Saipan Pacific/Tahiti Pacific/Tarawa Pacific/Tongatapu Pacific/Truk Pacific/Wake Pacific/Wallis";
  const ZONE_META = "0 0 1 2 1 0 3 0 0 4 3 4 5  6 0 0 1 1 3  0 4 4 7 4 1 4 4 3 3 3 0 3 4 4 3 4 7 7 1 0 1 3 3 0 0 3 0 8 2 4 9 a b b c d d d d d d d b e c f b c f b g h i d i g j k d l j m f i j d f  n g b 0 o n i p b i q f n c r s r p b b f t u r v w m p p m p p p p i p j d a p b x y z p b c f g b b m w d m f a f 10 r f 11 b p p a 12 m m m m j 13 n p b g b  m c f m q c 14 b c s a b 15 b b b b f f r z p b z o m a 16 17 18 19 1a 1b  1c 1d 1e 1f 6 1g 1h  1i 1h 1h 1j 1h 1g 1g 1k 1l 1m 5 1n 1o 1p 1q 1p  1r 1s 1t 1u 5 5 5 1v 1w 1x 1y 1z 20 21 1i 22 23 1q 1m 24 24 1g 25 26 27 28 1t 5 1m 1m 29 1h 1l 1y 2a 1g 1h 1h 2b 1g 1l 26 2c 2a 25 2d 26 2e 2c 2f 2g 2h 2i 1m 2j  2k 1l 2k 1q 2l 2m 2n r 2o 2p 2o 2o 0 2q 0 2r 2s 2t 2s 2u 2v 19 2t 2w 19 16 19 6 6 2x 5 6 6 6 6 5 6 6 5 6 2y 6 2z 5 30 31 32 8 5 33 2o 6 34 6 6 6 5 33 6 33 6 6 6 6 5 6 2x 6 6 2x 33 6 5 6 5 6 2x 6 6 6 5 33 6 6 6 1 35 36 37 1 38 39 3a 3b 1 3c 3d 1b  3e 3f 3g 3h 3i 3j 3k 3l 3m 3n 3o 3p 3q 3r 3s 3s 3t 3u 3v 3w 3x 3y 3u 3z 40 41 42 43 3o 44 45 46 47 48 49";
  const CUR_DIGITS = {"AFN":0,"ALL":0,"BHD":3,"BIF":0,"CLP":0,"COP":0,"DJF":0,"GNF":0,"HUF":0,"IDR":0,"IQD":0,"IRR":0,"ISK":0,"JOD":3,"JPY":0,"KMF":0,"KPW":0,"KRW":0,"KWD":3,"LAK":0,"LBP":0,"LYD":3,"MGA":0,"MMK":0,"OMR":3,"PKR":0,"PYG":0,"RWF":0,"SLL":0,"SOS":0,"SYP":0,"TND":3,"UGX":0,"VND":0,"VUV":0,"XAF":0,"XOF":0,"XPF":0,"YER":0};
  const UNITS = "acre bit byte celsius centimeter day degree fahrenheit fluid-ounce foot gallon gigabit gigabyte gram hectare hour inch kilobit kilobyte kilogram kilometer liter megabit megabyte meter microsecond mile mile-scandinavian milliliter millimeter millisecond minute month nanosecond ounce percent petabyte pound second stone terabit terabyte week yard year";
  const CURRENCIES = "AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BRL BSD BTN BWP BYN BZD CAD CDF CHF CLP CNY COP CRC CUC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD HKD HNL HRK HTG HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL LYD MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR RON RSD RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SLL SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS UAH UGX USD UYU UZS VES VND VUV WST XAF XCD XCG XDR XOF XPF XSU YER ZAR ZMW ZWG ZWL";
  const LOCALE_DATA = {
    "en": `{"n":{"Ml":["January","February","March","April","May","June","July","August","September","October","November","December"],"Ll":0,"El":["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"],"cl":0,"Gl":["Before Christ","Anno Domini"],"Bl":[0,"in the morning",12,"noon",13,"in the afternoon",18,"in the evening",21,"at night"],"Bl0":["in the morning","noon"],"Ms":["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"],"Ls":0,"Es":["Sun","Mon","Tue","Wed","Thu","Fri","Sat"],"cs":0,"Gs":["BC","AD"],"Bs":[0,"in the morning",12,"noon",13,"in the afternoon",18,"in the evening",21,"at night"],"Bs0":["in the morning","noon"],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["S","M","T","W","T","F","S"],"cn":0,"Gn":["B","A"],"Bn":[0,"in the morning",12,"n",13,"in the afternoon",18,"in the evening",21,"at night"],"Bn0":["in the morning","n"],"a":["AM","PM"]},"dp":[["","{d}","{M}","{M}/{d}","{M} {d}","{y}","{y} (day: {d})","{M}/{y}","{M}/{d}/{y}","{M} {y}","{M} {d}, {y}","{d} {y}","{M1}/{d1}/{y1} {G}","{G} (day: {d})","{G} (month: {M})","{M}/{d} {G}","{M} {d} {G}","{y} {G}","{y} {G} (day: {d})","{M}/{y} {G}","{M}/{d}/{y} {G}","{M} {y} {G}","{M} {d}, {y} {G}","{E}","{d} {E}","{M} {E}","{E}, {M}/{d}","{E}, {M} {d}","{E} (month: {M})","{y} {E}","{d} {E} {y}","{M}/{y} {E}","{E}, {M}/{d}/{y}","{M} {y} {E}","{E}, {M} {d}, {y}","{G} {E}","{d} {E} {G}","{E}, {M}/{d} {G}","{E}, {M} {d} {G}","{y} {G} {E}","{y} {G} (day: {d} {E})","{M}/{y} {G} {E}","{E}, {M}/{d}/{y} {G}","{M} {y} {G} {E}","{E}, {M} {d}, {y} {G}","{E} {y}","{d} {E} {y} {G}"],"0112332332442442445667887889::9::9::5;67887889::9::9::<==>??>??>@@>@@>@@ABBCDDCDDEFFEFFEFFABBCDDCDDEFFEFFEFFGHHIJJIJJIKKIKKLKKMNNOPPOPPQRRQRRQRRMNNOPPOPPQRRQRRQRRSTT>UU>UU>VV>VVSVVWXXYZZYZZ[]][]][]]WXXYZZYZZ[]][]][]]GHHIJJLJJLKKIKKLKKMNNOPPOPPQRRQRRQRR^NNOPPOPPQRRQRRQRRSTT>UUSUUSVV>VVSVVWXXYZZYZZ[]][]][]]W_XYZZYZZ[]][]][]]GHHIJJIJJIKKIKKIKKMNNOPPOPPQRRQRRQRRMNNOPPOPPQRRQRRQRRSTT>UU>UU>VV>VV>VVWXXYZZYZZ[]][]][]]WXXYZZYZZ[]][]][]]"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (second: {s})","{h} {a} {z} (second: {s})","{h} {a} (second: {s1})","{h} {a} {z} (second: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (second: {s})","{h2} {z} (second: {s})","{h2} (second: {s1})","{h2} {z} (second: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h2}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h2}:{m2}:{s} {z}","{h2}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h2}:{m}:{s2} {z}","{h2}:{m}:{s}","{h2}:{m}:{s} {z}","{h}","{h} {z}","{s} (hour: {h})","{h} {z} (second: {s})","{h} (second: {s1})","{h} {z} (second: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h}:{m} {z}","{h}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[]^_abcdef"],"wt":{"l":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}"]],"s":0,"n":0},"g":{"f":"{1} at {0}","l":"{1} at {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {Ml} {d1}, {y1}","{Ml} {d1}, {y1}","{Ms} {d1}, {y1}","{M1}/{d1}/{y2}"],"t12":["{h1}:{m2}:{s2} {a} {zl}","{h1}:{m2}:{s2} {a} {zs}","{h1}:{m2}:{s2} {a}","{h1}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} at {0}","{1} at {0}","{1}, {0}","{1}, {0}"]},"hc":"h12","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","Coordinated Universal Time"],"mz":{"1":["","","East Africa Time","","East Africa Time"],"3":["","","West Africa Time","","West Africa Time"],"5":["","","Eastern European Standard Time","Eastern European Summer Time","Eastern European Time"],"6":["","","Central European Standard Time","Central European Summer Time","Central European Time"],"7":["","","South Africa Standard Time","","South Africa Standard Time"],"10":["AKST","AKDT","Alaska Standard Time","Alaska Daylight Time","Alaska Time"],"12":["","","Brasilia Standard Time","","Brasilia Standard Time"],"13":["","","Argentina Standard Time","","Argentina Standard Time"],"15":["CST","","Central Standard Time","","Central Standard Time"],"17":["","","Colombia Standard Time","","Colombia Standard Time"],"18":["MST","MDT","Mountain Standard Time","Mountain Daylight Time","Mountain Time"],"20":["","","Venezuela Time","","Venezuela Time"],"22":["CST","CDT","Central Standard Time","Central Daylight Time","Central Time"],"23":["MST","","Mountain Standard Time","","Mountain Standard Time"],"25":["EST","EDT","Eastern Standard Time","Eastern Daylight Time","Eastern Time"],"27":["AST","ADT","Atlantic Standard Time","Atlantic Daylight Time","Atlantic Time"],"34":["","","Peru Standard Time","","Peru Standard Time"],"35":["PST","PDT","Pacific Standard Time","Pacific Daylight Time","Pacific Time"],"40":["","","Chile Standard Time","Chile Summer Time","Chile Time"],"41":["","","Newfoundland Standard Time","Newfoundland Daylight Time","Newfoundland Time"],"42":["","","Australian Western Standard Time","","Australian Western Standard Time"],"45":["","","Australian Eastern Standard Time","Australian Eastern Daylight Time","Australian Eastern Time"],"47":["","","New Zealand Standard Time","New Zealand Daylight Time","New Zealand Time"],"52":["","","Arabian Standard Time","","Arabian Standard Time"],"57":["","","Indochina Time","","Indochina Time"],"61":["","","India Standard Time","","India Standard Time"],"63":["","","Bangladesh Standard Time","","Bangladesh Standard Time"],"65":["","","Gulf Standard Time","","Gulf Standard Time"],"67":["","","Hong Kong Standard Time","","Hong Kong Standard Time"],"70":["","","Western Indonesia Time","","Western Indonesia Time"],"72":["","","Israel Standard Time","Israel Daylight Time","Israel Time"],"74":["","","Pakistan Standard Time","","Pakistan Standard Time"],"76":["","","Malaysia Time","","Malaysia Time"],"77":["","","China Standard Time","","China Standard Time"],"80":["","","Philippine Standard Time","","Philippine Standard Time"],"82":["","","Korean Standard Time","","Korean Standard Time"],"85":["","","Singapore Standard Time","","Singapore Standard Time"],"86":["","","Taiwan Standard Time","","Taiwan Standard Time"],"88":["","","Iran Standard Time","","Iran Standard Time"],"90":["","","Japan Standard Time","","Japan Standard Time"],"96":["","","Western European Standard Time","Western European Summer Time","Western European Time"],"100":["","","Australian Central Standard Time","Australian Central Daylight Time","Australian Central Time"],"101":["","","Australian Eastern Standard Time","","Australian Eastern Standard Time"],"106":["GMT","","Greenwich Mean Time","Irish Standard Time","Ireland Time"],"109":["","","Türkiye Standard Time","","Türkiye Standard Time"],"111":["","","Moscow Standard Time","","Moscow Standard Time"],"112":["GMT","","Greenwich Mean Time","British Summer Time","United Kingdom Time"],"133":["HST","","Hawaii-Aleutian Standard Time","","Hawaii-Aleutian Standard Time"]},"zg":{},"zs":{"America/Los_Angeles":"PT","America/Denver":"MT","America/Phoenix":"MST","America/Chicago":"CT","America/New_York":"ET","America/Anchorage":"AKT","Pacific/Honolulu":"HST","America/Halifax":"AT","America/St_Johns":"St. John’s Time","America/Toronto":"ET","America/Vancouver":"PT","America/Mexico_City":"CST","America/Sao_Paulo":"São Paulo Time","America/Buenos_Aires":"Buenos Aires Time","America/Bogota":"Colombia Time","America/Lima":"Peru Time","America/Santiago":"Chile Time","America/Caracas":"Venezuela Time","America/Costa_Rica":"CST","Europe/London":"United Kingdom Time","Europe/Dublin":"Ireland Time","Europe/Lisbon":"Portugal Time","Europe/Berlin":"Germany Time","Europe/Paris":"France Time","Europe/Madrid":"Spain Time","Europe/Rome":"Italy Time","Europe/Amsterdam":"Netherlands Time","Europe/Stockholm":"Sweden Time","Europe/Warsaw":"Poland Time","Europe/Zurich":"Switzerland Time","Europe/Vienna":"Austria Time","Europe/Prague":"Czechia Time","Europe/Athens":"Greece Time","Europe/Helsinki":"Finland Time","Europe/Kiev":"Ukraine Time","Europe/Istanbul":"Türkiye Time","Europe/Moscow":"Moscow Time","Africa/Cairo":"Egypt Time","Africa/Johannesburg":"South Africa Time","Africa/Lagos":"Nigeria Time","Africa/Nairobi":"Kenya Time","Asia/Dubai":"United Arab Emirates Time","Asia/Karachi":"Pakistan Time","Asia/Calcutta":"India Time","Asia/Dhaka":"Bangladesh Time","Asia/Bangkok":"Thailand Time","Asia/Jakarta":"Jakarta Time","Asia/Singapore":"Singapore Time","Asia/Manila":"Philippines Time","Asia/Hong_Kong":"Hong Kong SAR China Time","Asia/Shanghai":"China Time","Asia/Taipei":"Taiwan Time","Asia/Seoul":"South Korea Time","Asia/Tokyo":"Japan Time","Asia/Jerusalem":"Israel Time","Asia/Riyadh":"Saudi Arabia Time","Asia/Tehran":"Iran Time","Australia/Perth":"Perth Time","Australia/Adelaide":"Adelaide Time","Australia/Brisbane":"Brisbane Time","Australia/Sydney":"Sydney Time","Australia/Melbourne":"Melbourne Time","Pacific/Auckland":"New Zealand Time","Asia/Saigon":"Vietnam Time","Asia/Kuala_Lumpur":"Malaysia Time"},"rs":" – ","fs":".","num":{"dec":".","grp":",","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤#","-¤#"],"ca":["¤#","(¤#)"],"cc":["¤ #","-¤ #"],"cn":["# ¤","-# ¤"]},"cs":[[3,"#C","K"],[3,"#C","K"],[3,"#C","K"],[6,"#C","M"],[6,"#C","M"],[6,"#C","M"],[9,"#C","B"],[9,"#C","B"],[9,"#C","B"],[12,"#C","T"],[12,"#C","T"],[12,"#C","T"]],"cl":[[3,"# C","thousand"],[3,"# C","thousand"],[3,"# C","thousand"],[6,"# C","million"],[6,"# C","million"],[6,"# C","million"],[9,"# C","billion"],[9,"# C","billion"],[9,"# C","billion"],[12,"# C","trillion"],[12,"# C","trillion"],[12,"# C","trillion"]],"cur":{"USD":[["US dollar","US dollars"],"$"],"EUR":[["euro","euros"],"€"],"GBP":[["British pound","British pounds"],"£"],"JPY":["Japanese yen","¥"],"CNY":["Chinese yuan","CN¥","¥"],"INR":[["Indian rupee","Indian rupees"],"₹"],"CAD":[["Canadian dollar","Canadian dollars"],"CA$","$"],"AUD":[["Australian dollar","Australian dollars"],"A$","$"],"NZD":[["New Zealand dollar","New Zealand dollars"],"NZ$","$"],"CHF":[["Swiss franc","Swiss francs"]],"SEK":[["Swedish krona","Swedish kronor"],"SEK","kr"],"NOK":[["Norwegian krone","Norwegian kroner"],"NOK","kr"],"DKK":[["Danish krone","Danish kroner"],"DKK","kr"],"PLN":[["Polish zloty","Polish zlotys"],"PLN","zł"],"RUB":[["Russian ruble","Russian rubles"],"RUB","₽"],"BRL":[["Brazilian real","Brazilian reals"],"R$"],"MXN":[["Mexican peso","Mexican pesos"],"MX$","$"],"KRW":["South Korean won","₩"],"HKD":[["Hong Kong dollar","Hong Kong dollars"],"HK$","$"],"SGD":[["Singapore dollar","Singapore dollars"],"SGD","$"],"TWD":[["New Taiwan dollar","New Taiwan dollars"],"NT$","$"],"ZAR":["South African rand","ZAR","R"],"TRY":[["Turkish lira","Turkish Lira"],"TRY","₺"],"CZK":[["Czech koruna","Czech korunas"],"CZK","Kč"],"HUF":[["Hungarian forint","Hungarian forints"],"HUF","Ft"],"ILS":[["Israeli new shekel","Israeli new shekels"],"₪"],"AED":[["UAE dirham","UAE dirhams"]],"SAR":[["Saudi riyal","Saudi riyals"]],"THB":["Thai baht","THB","฿"],"IDR":[["Indonesian rupiah","Indonesian rupiahs"],"IDR","Rp"],"MYR":[["Malaysian ringgit","Malaysian ringgits"],"MYR","RM"],"PHP":[["Philippine peso","Philippine pesos"],"₱"],"VND":["Vietnamese dong","₫"],"UAH":[["Ukrainian hryvnia","Ukrainian hryvnias"],"UAH","₴"],"ARS":[["Argentine peso","Argentine pesos"],"ARS","$"],"CLP":[["Chilean peso","Chilean pesos"],"CLP","$"],"COP":[["Colombian peso","Colombian pesos"],"COP","$"],"EGP":[["Egyptian pound","Egyptian pounds"],"EGP","E£"],"NGN":[["Nigerian naira","Nigerian nairas"],"NGN","₦"],"PKR":[["Pakistani rupee","Pakistani rupees"],"PKR","Rs"],"BTC":["BTC"]},"unit":{"acre":{"l":["{0} acre","{0} acres"],"s":"{0} ac","n":"{0}ac"},"bit":{"l":["{0} bit","{0} bits"],"s":"{0} bit","n":"{0}bit"},"byte":{"l":["{0} byte","{0} bytes"],"s":"{0} byte","n":"{0}B"},"celsius":{"l":["{0} degree Celsius","{0} degrees Celsius"],"s":"{0}°C","n":0},"centimeter":{"l":["{0} centimeter","{0} centimeters"],"s":"{0} cm","n":"{0}cm"},"day":{"l":["{0} day","{0} days"],"s":["{0} day","{0} days"],"n":"{0}d"},"degree":{"l":["{0} degree","{0} degrees"],"s":"{0} deg","n":"{0}°"},"fahrenheit":{"l":["{0} degree Fahrenheit","{0} degrees Fahrenheit"],"s":"{0}°F","n":"{0}°"},"fluid-ounce":{"l":["{0} fluid ounce","{0} fluid ounces"],"s":"{0} fl oz","n":"{0}fl oz"},"foot":{"l":["{0} foot","{0} feet"],"s":"{0} ft","n":"{0}′"},"gallon":{"l":["{0} gallon","{0} gallons"],"s":"{0} gal","n":"{0}gal"},"gigabit":{"l":["{0} gigabit","{0} gigabits"],"s":"{0} Gb","n":"{0}Gb"},"gigabyte":{"l":["{0} gigabyte","{0} gigabytes"],"s":"{0} GB","n":"{0}GB"},"gram":{"l":["{0} gram","{0} grams"],"s":"{0} g","n":"{0}g"},"hectare":{"l":["{0} hectare","{0} hectares"],"s":"{0} ha","n":"{0}ha"},"hour":{"l":["{0} hour","{0} hours"],"s":"{0} hr","n":"{0}h"},"inch":{"l":["{0} inch","{0} inches"],"s":"{0} in","n":"{0}″"},"kilobit":{"l":["{0} kilobit","{0} kilobits"],"s":"{0} kb","n":"{0}kb"},"kilobyte":{"l":["{0} kilobyte","{0} kilobytes"],"s":"{0} kB","n":"{0}kB"},"kilogram":{"l":["{0} kilogram","{0} kilograms"],"s":"{0} kg","n":"{0}kg"},"kilometer":{"l":["{0} kilometer","{0} kilometers"],"s":"{0} km","n":"{0}km"},"liter":{"l":["{0} liter","{0} liters"],"s":"{0} L","n":"{0}L"},"megabit":{"l":["{0} megabit","{0} megabits"],"s":"{0} Mb","n":"{0}Mb"},"megabyte":{"l":["{0} megabyte","{0} megabytes"],"s":"{0} MB","n":"{0}MB"},"meter":{"l":["{0} meter","{0} meters"],"s":"{0} m","n":"{0}m"},"microsecond":{"l":["{0} microsecond","{0} microseconds"],"s":"{0} μs","n":"{0}μs"},"mile":{"l":["{0} mile","{0} miles"],"s":"{0} mi","n":"{0}mi"},"mile-scandinavian":{"l":["{0} mile-scandinavian","{0} miles-scandinavian"],"s":"{0} smi","n":"{0}smi"},"milliliter":{"l":["{0} milliliter","{0} milliliters"],"s":"{0} mL","n":"{0}mL"},"millimeter":{"l":["{0} millimeter","{0} millimeters"],"s":"{0} mm","n":"{0}mm"},"millisecond":{"l":["{0} millisecond","{0} milliseconds"],"s":"{0} ms","n":"{0}ms"},"minute":{"l":["{0} minute","{0} minutes"],"s":"{0} min","n":"{0}m"},"month":{"l":["{0} month","{0} months"],"s":["{0} mth","{0} mths"],"n":"{0}m"},"nanosecond":{"l":["{0} nanosecond","{0} nanoseconds"],"s":"{0} ns","n":"{0}ns"},"ounce":{"l":["{0} ounce","{0} ounces"],"s":"{0} oz","n":"{0}oz"},"percent":{"l":"{0} percent","s":"{0}%","n":0},"petabyte":{"l":["{0} petabyte","{0} petabytes"],"s":"{0} PB","n":"{0}PB"},"pound":{"l":["{0} pound","{0} pounds"],"s":"{0} lb","n":"{0}#"},"second":{"l":["{0} second","{0} seconds"],"s":"{0} sec","n":"{0}s"},"stone":{"l":["{0} stone","{0} stones"],"s":"{0} st","n":"{0}st"},"terabit":{"l":["{0} terabit","{0} terabits"],"s":"{0} Tb","n":"{0}Tb"},"terabyte":{"l":["{0} terabyte","{0} terabytes"],"s":"{0} TB","n":"{0}TB"},"week":{"l":["{0} week","{0} weeks"],"s":["{0} wk","{0} wks"],"n":"{0}w"},"yard":{"l":["{0} yard","{0} yards"],"s":"{0} yd","n":"{0}yd"},"year":{"l":["{0} year","{0} years"],"s":["{0} yr","{0} yrs"],"n":"{0}y"},"kilometer-per-hour":{"l":["{0} kilometer per hour","{0} kilometers per hour"],"s":"{0} km/h","n":"{0}km/h"},"mile-per-hour":{"l":["{0} mile per hour","{0} miles per hour"],"s":"{0} mph","n":"{0}mph"},"meter-per-second":{"l":["{0} meter per second","{0} meters per second"],"s":"{0} m/s","n":"{0}m/s"},"kilobit-per-second":{"l":["{0} kilobit per second","{0} kilobits per second"],"s":"{0} kb/s","n":"{0}kb/s"},"megabit-per-second":{"l":["{0} megabit per second","{0} megabits per second"],"s":"{0} Mb/s","n":"{0}Mb/s"},"megabyte-per-second":{"l":["{0} megabyte per second","{0} megabytes per second"],"s":"{0} MB/s","n":"{0}MB/s"}},"rng":"{0}–{1}","approx":"~","rngS":" – "},"rel":{"l":{"year":[["in {0} year","in {0} years"],["{0} year ago","{0} years ago"],{"0":"this year","1":"next year","-1":"last year"}],"quarter":[["in {0} quarter","in {0} quarters"],["{0} quarter ago","{0} quarters ago"],{"0":"this quarter","1":"next quarter","-1":"last quarter"}],"month":[["in {0} month","in {0} months"],["{0} month ago","{0} months ago"],{"0":"this month","1":"next month","-1":"last month"}],"week":[["in {0} week","in {0} weeks"],["{0} week ago","{0} weeks ago"],{"0":"this week","1":"next week","-1":"last week"}],"day":[["in {0} day","in {0} days"],["{0} day ago","{0} days ago"],{"0":"today","1":"tomorrow","-1":"yesterday"}],"hour":[["in {0} hour","in {0} hours"],["{0} hour ago","{0} hours ago"],{"0":"this hour"}],"minute":[["in {0} minute","in {0} minutes"],["{0} minute ago","{0} minutes ago"],{"0":"this minute"}],"second":[["in {0} second","in {0} seconds"],["{0} second ago","{0} seconds ago"],{"0":"now"}]},"s":{"year":["in {0} yr.","{0} yr. ago",{"0":"this yr.","1":"next yr.","-1":"last yr."}],"quarter":[["in {0} qtr.","in {0} qtrs."],["{0} qtr. ago","{0} qtrs. ago"],{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":["in {0} mo.","{0} mo. ago",{"0":"this mo.","1":"next mo.","-1":"last mo."}],"week":["in {0} wk.","{0} wk. ago",{"0":"this wk.","1":"next wk.","-1":"last wk."}],"day":[["in {0} day","in {0} days"],["{0} day ago","{0} days ago"],{"0":"today","1":"tomorrow","-1":"yesterday"}],"hour":["in {0} hr.","{0} hr. ago",{"0":"this hour"}],"minute":["in {0} min.","{0} min. ago",{"0":"this minute"}],"second":["in {0} sec.","{0} sec. ago",{"0":"now"}]},"n":{"year":["in {0}y","{0}y ago",{"0":"this yr.","1":"next yr.","-1":"last yr."}],"quarter":["in {0}q","{0}q ago",{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":["in {0}mo","{0}mo ago",{"0":"this mo.","1":"next mo.","-1":"last mo."}],"week":["in {0}w","{0}w ago",{"0":"this wk.","1":"next wk.","-1":"last wk."}],"day":["in {0}d","{0}d ago",{"0":"today","1":"tomorrow","-1":"yesterday"}],"hour":["in {0}h","{0}h ago",{"0":"this hour"}],"minute":["in {0}m","{0}m ago",{"0":"this minute"}],"second":["in {0}s","{0}s ago",{"0":"now"}]}},"list":{"cl":["{0} and {1}","{0}, {1}","{0}, {1}","{0}, and {1}"],"cs":["{0} & {1}","{0}, {1}","{0}, {1}","{0}, & {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} or {1}","{0}, {1}","{0}, {1}","{0}, or {1}"],"ds":["{0} or {1}","{0}, {1}","{0}, {1}","{0}, or {1}"],"dn":["{0} or {1}","{0}, {1}","{0}, {1}","{0}, or {1}"],"ul":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"us":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"lang":{"aa":"Afar","ab":"Abkhazian","ae":"Avestan","af":"Afrikaans","ak":"Akan","am":"Amharic","an":"Aragonese","ar":"Arabic","as":"Assamese","av":"Avaric","ay":"Aymara","az":"Azerbaijani","ba":"Bashkir","be":"Belarusian","bg":"Bulgarian","bh":"Bhojpuri","bi":"Bislama","bm":"Bambara","bn":"Bangla","bo":"Tibetan","br":"Breton","bs":"Bosnian","ca":"Catalan","ce":"Chechen","ch":"Chamorro","co":"Corsican","cr":"Cree","cs":"Czech","cu":"Church Slavic","cv":"Chuvash","cy":"Welsh","da":"Danish","de":"German","dv":"Divehi","dz":"Dzongkha","ee":"Ewe","el":"Greek","en":"English","eo":"Esperanto","es":"Spanish","et":"Estonian","eu":"Basque","fa":"Persian","ff":"Fula","fi":"Finnish","fj":"Fijian","fo":"Faroese","fr":"French","fy":"Western Frisian","ga":"Irish","gd":"Scottish Gaelic","gl":"Galician","gn":"Guarani","gu":"Gujarati","gv":"Manx","ha":"Hausa","he":"Hebrew","hi":"Hindi","ho":"Hiri Motu","hr":"Croatian","ht":"Haitian Creole","hu":"Hungarian","hy":"Armenian","hz":"Herero","ia":"Interlingua","id":"Indonesian","ie":"Interlingue","ig":"Igbo","ii":"Sichuan Yi","ik":"Inupiaq","in":"Indonesian","io":"Ido","is":"Icelandic","it":"Italian","iu":"Inuktitut","iw":"Hebrew","ja":"Japanese","ji":"Yiddish","jv":"Javanese","jw":"Javanese","ka":"Georgian","kg":"Kongo","ki":"Kikuyu","kj":"Kuanyama","kk":"Kazakh","kl":"Kalaallisut","km":"Khmer","kn":"Kannada","ko":"Korean","kr":"Kanuri","ks":"Kashmiri","ku":"Kurdish","kv":"Komi","kw":"Cornish","ky":"Kyrgyz","la":"Latin","lb":"Luxembourgish","lg":"Ganda","li":"Limburgish","ln":"Lingala","lo":"Lao","lt":"Lithuanian","lu":"Luba-Katanga","lv":"Latvian","mg":"Malagasy","mh":"Marshallese","mi":"Māori","mk":"Macedonian","ml":"Malayalam","mn":"Mongolian","mo":"Romanian","mr":"Marathi","ms":"Malay","mt":"Maltese","my":"Burmese","na":"Nauru","nb":"Norwegian Bokmål","nd":"North Ndebele","ne":"Nepali","ng":"Ndonga","nl":"Dutch","nn":"Norwegian Nynorsk","no":"Norwegian","nr":"South Ndebele","nv":"Navajo","ny":"Nyanja","oc":"Occitan","oj":"Ojibwa","om":"Oromo","or":"Odia","os":"Ossetic","pa":"Punjabi","pi":"Pali","pl":"Polish","ps":"Pashto","pt":"Portuguese","qu":"Quechua","rm":"Romansh","rn":"Rundi","ro":"Romanian","ru":"Russian","rw":"Kinyarwanda","sa":"Sanskrit","sc":"Sardinian","sd":"Sindhi","se":"Northern Sami","sg":"Sango","sh":"Serbian (Latin)","si":"Sinhala","sk":"Slovak","sl":"Slovenian","sm":"Samoan","sn":"Shona","so":"Somali","sq":"Albanian","sr":"Serbian","ss":"Swati","st":"Southern Sotho","su":"Sundanese","sv":"Swedish","sw":"Swahili","ta":"Tamil","te":"Telugu","tg":"Tajik","th":"Thai","ti":"Tigrinya","tk":"Turkmen","tl":"Filipino","tn":"Tswana","to":"Tongan","tr":"Turkish","ts":"Tsonga","tt":"Tatar","tw":"Akan","ty":"Tahitian","ug":"Uyghur","uk":"Ukrainian","ur":"Urdu","uz":"Uzbek","ve":"Venda","vi":"Vietnamese","vo":"Volapük","wa":"Walloon","wo":"Wolof","xh":"Xhosa","yi":"Yiddish","yo":"Yoruba","za":"Zhuang","zh":"Chinese","zu":"Zulu","tlh":"Klingon","ace":"Acehnese","ast":"Asturian","bal":"Baluchi","ban":"Balinese","bem":"Bemba","bho":"Bhojpuri","ceb":"Cebuano","chr":"Cherokee","ckb":"Central Kurdish","cop":"Coptic","dsb":"Lower Sorbian","fil":"Filipino","fur":"Friulian","gsw":"Swiss German","haw":"Hawaiian","hmn":"Hmong","hsb":"Upper Sorbian","ilo":"Iloko","jbo":"Lojban","kab":"Kabyle","kok":"Konkani","lkt":"Lakota","lou":"Louisiana Creole","mai":"Maithili","mni":"Manipuri","nds":"Low German","nqo":"N’Ko","pcm":"Nigerian Pidgin","sah":"Yakut","sat":"Santali","scn":"Sicilian","sco":"Scots","shn":"Shan","smj":"Lule Sami","smn":"Inari Sami","syr":"Syriac","tok":"Toki Pona","tzm":"Central Atlas Tamazight","yue":"Cantonese","zgh":"Standard Moroccan Tamazight","zza":"Zaza","arz":"Egyptian Arabic","nan":"Min Nan Chinese","hak":"Hakka Chinese","cmn":"Chinese","wuu":"Wu Chinese","ar-001":"Modern Standard Arabic","de-AT":"Austrian German","de-CH":"Swiss High German","en-AU":"Australian English","en-CA":"Canadian English","en-GB":"British English","en-US":"American English","es-419":"Latin American Spanish","es-ES":"European Spanish","es-MX":"Mexican Spanish","fa-AF":"Dari","fr-CA":"Canadian French","fr-CH":"Swiss French","nds-NL":"Low Saxon","nl-BE":"Flemish","pt-BR":"Brazilian Portuguese","pt-PT":"European Portuguese","ro-MD":"Moldavian","sw-CD":"Congo Swahili","zh-Hans":"Simplified Chinese","zh-Hant":"Traditional Chinese","en-IN":"English (India)","en-NZ":"English (New Zealand)","zh-CN":"Chinese (China)","zh-TW":"Chinese (Taiwan)"},"langS":{"az":"Azeri","en-GB":"UK English","en-US":"US English"},"reg":{"142":"Asia","143":"Central Asia","145":"Western Asia","150":"Europe","151":"Eastern Europe","154":"Northern Europe","155":"Western Europe","202":"Sub-Saharan Africa","419":"Latin America","AC":"Ascension Island","AD":"Andorra","AE":"United Arab Emirates","AF":"Afghanistan","AG":"Antigua & Barbuda","AI":"Anguilla","AL":"Albania","AM":"Armenia","AN":"Curaçao","AO":"Angola","AQ":"Antarctica","AR":"Argentina","AS":"American Samoa","AT":"Austria","AU":"Australia","AW":"Aruba","AX":"Åland Islands","AZ":"Azerbaijan","BA":"Bosnia & Herzegovina","BB":"Barbados","BD":"Bangladesh","BE":"Belgium","BF":"Burkina Faso","BG":"Bulgaria","BH":"Bahrain","BI":"Burundi","BJ":"Benin","BL":"St. Barthélemy","BM":"Bermuda","BN":"Brunei","BO":"Bolivia","BQ":"Caribbean Netherlands","BR":"Brazil","BS":"Bahamas","BT":"Bhutan","BU":"Myanmar (Burma)","BV":"Bouvet Island","BW":"Botswana","BY":"Belarus","BZ":"Belize","CA":"Canada","CC":"Cocos (Keeling) Islands","CD":"Congo - Kinshasa","CF":"Central African Republic","CG":"Congo - Brazzaville","CH":"Switzerland","CI":"Côte d’Ivoire","CK":"Cook Islands","CL":"Chile","CM":"Cameroon","CN":"China","CO":"Colombia","CP":"Clipperton Island","CQ":"Sark","CR":"Costa Rica","CS":"Serbia","CU":"Cuba","CV":"Cape Verde","CW":"Curaçao","CX":"Christmas Island","CY":"Cyprus","CZ":"Czechia","DD":"Germany","DE":"Germany","DG":"Diego Garcia","DJ":"Djibouti","DK":"Denmark","DM":"Dominica","DO":"Dominican Republic","DY":"Benin","DZ":"Algeria","EA":"Ceuta & Melilla","EC":"Ecuador","EE":"Estonia","EG":"Egypt","EH":"Western Sahara","ER":"Eritrea","ES":"Spain","ET":"Ethiopia","EU":"European Union","EZ":"Eurozone","FI":"Finland","FJ":"Fiji","FK":"Falkland Islands","FM":"Micronesia","FO":"Faroe Islands","FR":"France","FX":"France","GA":"Gabon","GB":"United Kingdom","GD":"Grenada","GE":"Georgia","GF":"French Guiana","GG":"Guernsey","GH":"Ghana","GI":"Gibraltar","GL":"Greenland","GM":"Gambia","GN":"Guinea","GP":"Guadeloupe","GQ":"Equatorial Guinea","GR":"Greece","GS":"South Georgia & South Sandwich Islands","GT":"Guatemala","GU":"Guam","GW":"Guinea-Bissau","GY":"Guyana","HK":"Hong Kong SAR China","HM":"Heard & McDonald Islands","HN":"Honduras","HR":"Croatia","HT":"Haiti","HU":"Hungary","HV":"Burkina Faso","IC":"Canary Islands","ID":"Indonesia","IE":"Ireland","IL":"Israel","IM":"Isle of Man","IN":"India","IO":"British Indian Ocean Territory","IQ":"Iraq","IR":"Iran","IS":"Iceland","IT":"Italy","JE":"Jersey","JM":"Jamaica","JO":"Jordan","JP":"Japan","KE":"Kenya","KG":"Kyrgyzstan","KH":"Cambodia","KI":"Kiribati","KM":"Comoros","KN":"St. Kitts & Nevis","KP":"North Korea","KR":"South Korea","KW":"Kuwait","KY":"Cayman Islands","KZ":"Kazakhstan","LA":"Laos","LB":"Lebanon","LC":"St. Lucia","LI":"Liechtenstein","LK":"Sri Lanka","LR":"Liberia","LS":"Lesotho","LT":"Lithuania","LU":"Luxembourg","LV":"Latvia","LY":"Libya","MA":"Morocco","MC":"Monaco","MD":"Moldova","ME":"Montenegro","MF":"St. Martin","MG":"Madagascar","MH":"Marshall Islands","MK":"North Macedonia","ML":"Mali","MM":"Myanmar (Burma)","MN":"Mongolia","MO":"Macao SAR China","MP":"Northern Mariana Islands","MQ":"Martinique","MR":"Mauritania","MS":"Montserrat","MT":"Malta","MU":"Mauritius","MV":"Maldives","MW":"Malawi","MX":"Mexico","MY":"Malaysia","MZ":"Mozambique","NA":"Namibia","NC":"New Caledonia","NE":"Niger","NF":"Norfolk Island","NG":"Nigeria","NH":"Vanuatu","NI":"Nicaragua","NL":"Netherlands","NO":"Norway","NP":"Nepal","NR":"Nauru","NU":"Niue","NZ":"New Zealand","OM":"Oman","PA":"Panama","PE":"Peru","PF":"French Polynesia","PG":"Papua New Guinea","PH":"Philippines","PK":"Pakistan","PL":"Poland","PM":"St. Pierre & Miquelon","PN":"Pitcairn Islands","PR":"Puerto Rico","PS":"Palestinian Territories","PT":"Portugal","PW":"Palau","PY":"Paraguay","QA":"Qatar","QO":"Outlying Oceania","RE":"Réunion","RH":"Zimbabwe","RO":"Romania","RS":"Serbia","RU":"Russia","RW":"Rwanda","SA":"Saudi Arabia","SB":"Solomon Islands","SC":"Seychelles","SD":"Sudan","SE":"Sweden","SG":"Singapore","SH":"St. Helena","SI":"Slovenia","SJ":"Svalbard & Jan Mayen","SK":"Slovakia","SL":"Sierra Leone","SM":"San Marino","SN":"Senegal","SO":"Somalia","SR":"Suriname","SS":"South Sudan","ST":"São Tomé & Príncipe","SU":"Russia","SV":"El Salvador","SX":"Sint Maarten","SY":"Syria","SZ":"Eswatini","TA":"Tristan da Cunha","TC":"Turks & Caicos Islands","TD":"Chad","TF":"French Southern Territories","TG":"Togo","TH":"Thailand","TJ":"Tajikistan","TK":"Tokelau","TL":"Timor-Leste","TM":"Turkmenistan","TN":"Tunisia","TO":"Tonga","TP":"Timor-Leste","TR":"Türkiye","TT":"Trinidad & Tobago","TV":"Tuvalu","TW":"Taiwan","TZ":"Tanzania","UA":"Ukraine","UG":"Uganda","UK":"United Kingdom","UM":"U.S. Outlying Islands","UN":"United Nations","US":"United States","UY":"Uruguay","UZ":"Uzbekistan","VA":"Vatican City","VC":"St. Vincent & Grenadines","VD":"Vietnam","VE":"Venezuela","VG":"British Virgin Islands","VI":"U.S. Virgin Islands","VN":"Vietnam","VU":"Vanuatu","WF":"Wallis & Futuna","WS":"Samoa","XA":"Pseudo-Accents","XB":"Pseudo-Bidi","XK":"Kosovo","YD":"Yemen","YE":"Yemen","YT":"Mayotte","YU":"Serbia","ZA":"South Africa","ZM":"Zambia","ZR":"Congo - Kinshasa","ZW":"Zimbabwe","ZZ":"Unknown Region","001":"world","002":"Africa","003":"North America","005":"South America","009":"Oceania","011":"Western Africa","013":"Central America","014":"Eastern Africa","015":"Northern Africa","017":"Middle Africa","018":"Southern Africa","019":"Americas","021":"Northern America","029":"Caribbean","030":"Eastern Asia","034":"Southern Asia","035":"Southeast Asia","039":"Southern Europe","053":"Australasia","054":"Melanesia","057":"Micronesian Region","061":"Polynesia"},"regS":{"BA":"Bosnia","CC":"Cocos Islands","GB":"UK","HK":"Hong Kong","MM":"Myanmar","MO":"Macao","PN":"Pitcairn","PS":"Palestine","UN":"UN","US":"US"},"cur":{"AED":"United Arab Emirates Dirham","AFN":"Afghan Afghani","ALL":"Albanian Lek","AMD":"Armenian Dram","ANG":"Netherlands Antillean Guilder","AOA":"Angolan Kwanza","ARS":"Argentine Peso","AUD":"Australian Dollar","AWG":"Aruban Florin","AZN":"Azerbaijani Manat","BAM":"Bosnia-Herzegovina Convertible Mark","BBD":"Barbadian Dollar","BDT":"Bangladeshi Taka","BGN":"Bulgarian Lev","BHD":"Bahraini Dinar","BIF":"Burundian Franc","BMD":"Bermudan Dollar","BND":"Brunei Dollar","BOB":"Bolivian Boliviano","BRL":"Brazilian Real","BSD":"Bahamian Dollar","BTN":"Bhutanese Ngultrum","BWP":"Botswanan Pula","BYN":"Belarusian Ruble","BZD":"Belize Dollar","CAD":"Canadian Dollar","CDF":"Congolese Franc","CHF":"Swiss Franc","CLP":"Chilean Peso","CNY":"Chinese Yuan","COP":"Colombian Peso","CRC":"Costa Rican Colón","CUC":"Cuban Convertible Peso","CUP":"Cuban Peso","CVE":"Cape Verdean Escudo","CZK":"Czech Koruna","DJF":"Djiboutian Franc","DKK":"Danish Krone","DOP":"Dominican Peso","DZD":"Algerian Dinar","EGP":"Egyptian Pound","ERN":"Eritrean Nakfa","ETB":"Ethiopian Birr","EUR":"Euro","FJD":"Fijian Dollar","FKP":"Falkland Islands Pound","GBP":"British Pound","GEL":"Georgian Lari","GHS":"Ghanaian Cedi","GIP":"Gibraltar Pound","GMD":"Gambian Dalasi","GNF":"Guinean Franc","GTQ":"Guatemalan Quetzal","GYD":"Guyanaese Dollar","HKD":"Hong Kong Dollar","HNL":"Honduran Lempira","HRK":"Croatian Kuna","HTG":"Haitian Gourde","HUF":"Hungarian Forint","IDR":"Indonesian Rupiah","ILS":"Israeli New Shekel","INR":"Indian Rupee","IQD":"Iraqi Dinar","IRR":"Iranian Rial","ISK":"Icelandic Króna","JMD":"Jamaican Dollar","JOD":"Jordanian Dinar","JPY":"Japanese Yen","KES":"Kenyan Shilling","KGS":"Kyrgyz Som","KHR":"Cambodian Riel","KMF":"Comorian Franc","KPW":"North Korean Won","KRW":"South Korean Won","KWD":"Kuwaiti Dinar","KYD":"Cayman Islands Dollar","KZT":"Kazakhstani Tenge","LAK":"Laotian Kip","LBP":"Lebanese Pound","LKR":"Sri Lankan Rupee","LRD":"Liberian Dollar","LSL":"Lesotho Loti","LYD":"Libyan Dinar","MAD":"Moroccan Dirham","MDL":"Moldovan Leu","MGA":"Malagasy Ariary","MKD":"Macedonian Denar","MMK":"Myanmar Kyat","MNT":"Mongolian Tugrik","MOP":"Macanese Pataca","MRU":"Mauritanian Ouguiya","MUR":"Mauritian Rupee","MVR":"Maldivian Rufiyaa","MWK":"Malawian Kwacha","MXN":"Mexican Peso","MYR":"Malaysian Ringgit","MZN":"Mozambican Metical","NAD":"Namibian Dollar","NGN":"Nigerian Naira","NIO":"Nicaraguan Córdoba","NOK":"Norwegian Krone","NPR":"Nepalese Rupee","NZD":"New Zealand Dollar","OMR":"Omani Rial","PAB":"Panamanian Balboa","PEN":"Peruvian Sol","PGK":"Papua New Guinean Kina","PHP":"Philippine Peso","PKR":"Pakistani Rupee","PLN":"Polish Zloty","PYG":"Paraguayan Guarani","QAR":"Qatari Riyal","RON":"Romanian Leu","RSD":"Serbian Dinar","RUB":"Russian Ruble","RWF":"Rwandan Franc","SAR":"Saudi Riyal","SBD":"Solomon Islands Dollar","SCR":"Seychellois Rupee","SDG":"Sudanese Pound","SEK":"Swedish Krona","SGD":"Singapore Dollar","SHP":"St. Helena Pound","SLE":"Sierra Leonean Leone","SLL":"Sierra Leonean Leone (1964—2022)","SOS":"Somali Shilling","SRD":"Surinamese Dollar","SSP":"South Sudanese Pound","STN":"São Tomé & Príncipe Dobra","SVC":"Salvadoran Colón","SYP":"Syrian Pound","SZL":"Swazi Lilangeni","THB":"Thai Baht","TJS":"Tajikistani Somoni","TMT":"Turkmenistani Manat","TND":"Tunisian Dinar","TOP":"Tongan Paʻanga","TRY":"Turkish Lira","TTD":"Trinidad & Tobago Dollar","TWD":"New Taiwan Dollar","TZS":"Tanzanian Shilling","UAH":"Ukrainian Hryvnia","UGX":"Ugandan Shilling","USD":"US Dollar","UYU":"Uruguayan Peso","UZS":"Uzbekistani Som","VES":"Venezuelan Bolívar","VND":"Vietnamese Dong","VUV":"Vanuatu Vatu","WST":"Samoan Tala","XAF":"Central African CFA Franc","XCD":"East Caribbean Dollar","XCG":"Caribbean guilder","XDR":"Special Drawing Rights","XOF":"West African CFA Franc","XPF":"CFP Franc","XSU":"Sucre","YER":"Yemeni Rial","ZAR":"South African Rand","ZMW":"Zambian Kwacha","ZWG":"Zimbabwean Gold","ZWL":"Zimbabwean Dollar (2009–2024)","XXX":"Unknown Currency"},"scr":{"Arab":"Arabic","Armn":"Armenian","Beng":"Bangla","Bopo":"Bopomofo","Brai":"Braille","Cyrl":"Cyrillic","Deva":"Devanagari","Ethi":"Ethiopic","Geor":"Georgian","Grek":"Greek","Gujr":"Gujarati","Guru":"Gurmukhi","Hang":"Hangul","Hani":"Han","Hans":"Simplified","Hant":"Traditional","Hebr":"Hebrew","Hira":"Hiragana","Jpan":"Japanese","Kana":"Katakana","Khmr":"Khmer","Knda":"Kannada","Kore":"Korean","Laoo":"Lao","Latn":"Latin","Mlym":"Malayalam","Mong":"Mongolian","Mymr":"Myanmar","Orya":"Odia","Sinh":"Sinhala","Taml":"Tamil","Telu":"Telugu","Thaa":"Thaana","Tibt":"Tibetan","Zmth":"Mathematical Notation","Zsye":"Emoji","Zsym":"Symbols","Zxxx":"Unwritten","Zyyy":"Common","Zzzz":"Unknown Script"},"cal":{"buddhist":"Buddhist Calendar","chinese":"Chinese Calendar","coptic":"Coptic Calendar","dangi":"Dangi Calendar","ethioaa":"Ethiopic Amete Alem Calendar","ethiopic":"Ethiopic Calendar","gregory":"Gregorian Calendar","hebrew":"Hebrew Calendar","indian":"Indian National Calendar","islamic":"Hijri Calendar","islamic-civil":"Hijri Calendar (tabular, civil epoch)","islamic-rgsa":"Hijri Calendar (Saudi Arabia, sighting)","islamic-tbla":"Hijri Calendar (tabular, astronomical epoch)","islamic-umalqura":"Hijri Calendar (Umm al-Qura)","iso8601":"Gregorian Calendar (ISO 8601 Weeks)","japanese":"Japanese Calendar","persian":"Persian Calendar","roc":"Minguo Calendar"},"fld":{"l":["era","year","quarter","month","week","day of the week","day","AM/PM","hour","minute","second","time zone"],"s":["era","yr.","qtr.","mo.","wk.","day of wk.","day","AM/PM","hr.","min.","sec.","zone"],"n":["era","yr","qtr","mo","wk","day of wk.","day","AM/PM","hr","min","sec","zone"]},"lp":["{0} ({1})",", "]}}`,
    "en-GB": `{"n":{"Ms":["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sept","Oct","Nov","Dec"],"a":["am","pm"]},"dp":[["","{d}","{M}","{d2}/{M2}","{d}/{M}","{d} {M}","{y}","{y} (day: {d})","{M2}/{y}","{d2}/{M2}/{y}","{d}/{M}/{y}","{M}/{y}","{M} {y}","{d} {M} {y}","{d} {y}","{d2}/{M2}/{y1} {G}","{G} (day: {d})","{G} (month: {M})","{d2}/{M2} {G}","{d}/{M} {G}","{d} {M} {G}","{y} {G}","{y} {G} (day: {d})","{M}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E} {d2}/{M2}","{E} {d}/{M2}","{E} {d2}/{M}","{E} {d}/{M}","{E} {d} {M}","{E} (month: {M})","{y} {E}","{E} {d} {y}","{M2}/{y} {E}","{E}, {d2}/{M2}/{y}","{E}, {d}/{M2}/{y}","{M}/{y} {E}","{E}, {d2}/{M}/{y}","{E}, {d}/{M}/{y}","{M} {y} {E}","{E}, {d} {M} {y}","{G} {E}","{E} {d} {G}","{E} {d2}/{M2} {G}","{E} {d}/{M2} {G}","{E} {d2}/{M} {G}","{E} {d}/{M} {G}","{E} {d} {M} {G}","{y} {G} {E}","{y} {G} (day: {E} {d})","{M}/{y} {G} {E}","{E}, {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E}, {d} {M} {y} {G}","{E} {y}","{E} {d} {y} {G}"],"01123424425525525567789:;::<==<==<==6>789:;::<==<==<==?@@ABCACCADDADDADDEFFGHIGJKLMMLMMLMMEFFGHIGJKLMMLMMLMMNOOPQRPSTPUUPUUVUUWXXYZ[]^_abbabbabbWXXYZ[]^_abbabbabbcddAefAghAiiAiiciijkklmmlmmnoonoonoojkklmmlmmnoonoonooNOOPQRVSTVUUPUUVUUWXXYZ[]^_abbabbabbpXXYZ[]^_abbabbabbcddAefcghciiAiiciijkklmmlmmnoonoonoojqklmmlmmnoonoonooNOOPQRPSTPUUPUUPUUWXXYZ[]^_abbabbabbWXXYZ[]^_abbabbabbcddAefAghAiiAiiAiijkklmmlmmnoonoonoojkklmmlmmnoonoonoo"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (second: {s})","{h2} {z} (second: {s})","{h2} (second: {s1})","{h2} {z} (second: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h}","{h} {z}","{s} (hour: {h})","{h} {z} (second: {s})","{h} (second: {s1})","{h} {z} (second: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]]},"st":{"d":["{El}, {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d2}/{M2}/{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"]},"hc":"h23","mz":{"5":["EET","EEST","Eastern European Standard Time","Eastern European Summer Time","Eastern European Time"],"6":["CET","CEST","Central European Standard Time","Central European Summer Time","Central European Time"],"10":["","","Alaska Standard Time","Alaska Daylight Time","Alaska Time"],"15":["","","Central Standard Time","","Central Standard Time"],"18":["","","Mountain Standard Time","Mountain Daylight Time","Mountain Time"],"22":["","","Central Standard Time","Central Daylight Time","Central Time"],"23":["","","Mountain Standard Time","","Mountain Standard Time"],"25":["","","Eastern Standard Time","Eastern Daylight Time","Eastern Time"],"27":["","","Atlantic Standard Time","Atlantic Daylight Time","Atlantic Time"],"35":["","","Pacific Standard Time","Pacific Daylight Time","Pacific Time"],"65":["GST","","Gulf Standard Time","","Gulf Standard Time"],"96":["WET","WEST","Western European Standard Time","Western European Summer Time","Western European Time"],"112":["GMT","BST","Greenwich Mean Time","British Summer Time","United Kingdom Time"],"133":["","","Hawaii-Aleutian Standard Time","","Hawaii-Aleutian Standard Time"]},"zs":{"America/Los_Angeles":"Los Angeles Time","America/Denver":"Denver Time","America/Phoenix":"Phoenix Time","America/Chicago":"Chicago Time","America/New_York":"New York Time","America/Anchorage":"Anchorage Time","Pacific/Honolulu":"Honolulu Time","America/Halifax":"Halifax Time","America/St_Johns":"St John’s Time","America/Toronto":"Toronto Time","America/Vancouver":"Vancouver Time","America/Mexico_City":"Mexico City Time","America/Costa_Rica":"Costa Rica Time","Europe/Lisbon":"WET","Europe/Berlin":"CET","Europe/Paris":"CET","Europe/Madrid":"CET","Europe/Rome":"CET","Europe/Amsterdam":"CET","Europe/Stockholm":"CET","Europe/Warsaw":"CET","Europe/Zurich":"CET","Europe/Vienna":"CET","Europe/Prague":"CET","Europe/Athens":"EET","Europe/Helsinki":"EET","Europe/Kiev":"EET","Africa/Cairo":"EET","Asia/Dubai":"GST"},"num":{"cs":[[3,"#C","k"],[3,"#C","k"],[3,"#C","k"],[6,"#C","m"],[6,"#C","m"],[6,"#C","m"],[9,"#C","bn"],[9,"#C","bn"],[9,"#C","bn"],[12,"#C","tn"],[12,"#C","tn"],[12,"#C","tn"]],"cur":{"USD":[["US dollar","US dollars"],"US$","$"],"JPY":["Japanese yen","JP¥","¥"],"RUB":[["Russian rouble","Russian roubles"],"RUB","₽"]},"unit":{"centimeter":{"l":["{0} centimetre","{0} centimetres"],"s":"{0} cm","n":"{0}cm"},"fahrenheit":{"l":["{0} degree Fahrenheit","{0} degrees Fahrenheit"],"s":"{0}°F","n":0},"fluid-ounce":{"l":["{0} US fluid ounce","{0} US fluid ounces"],"s":"{0} US fl oz","n":"{0}US fl oz"},"gallon":{"l":["{0} US gallon","{0} US gallons"],"s":"{0} US gal","n":"{0}USgal"},"hour":{"l":["{0} hour","{0} hours"],"s":["{0} hr","{0} hrs"],"n":"{0}h"},"kilometer":{"l":["{0} kilometre","{0} kilometres"],"s":"{0} km","n":"{0}km"},"liter":{"l":["{0} litre","{0} litres"],"s":"{0} l","n":"{0}l"},"meter":{"l":["{0} metre","{0} metres"],"s":"{0} m","n":"{0}m"},"mile-scandinavian":{"l":["{0} Scandinavian mile","{0} Scandinavian miles"],"s":"{0} smi","n":"{0}smi"},"milliliter":{"l":["{0} millilitre","{0} millilitres"],"s":"{0} ml","n":"{0}ml"},"millimeter":{"l":["{0} millimetre","{0} millimetres"],"s":"{0} mm","n":"{0}mm"},"minute":{"l":["{0} minute","{0} minutes"],"s":["{0} min","{0} mins"],"n":"{0}m"},"percent":{"l":"{0} per cent","s":"{0}%","n":0},"pound":{"l":["{0} pound","{0} pounds"],"s":"{0} lb","n":"{0}lb"},"second":{"l":["{0} second","{0} seconds"],"s":["{0} sec","{0} secs"],"n":"{0}s"},"stone":{"l":"{0} stone","s":"{0} st","n":"{0}st"},"kilometer-per-hour":{"l":["{0} kilometre per hour","{0} kilometres per hour"],"s":"{0} km/h","n":"{0}km/h"},"meter-per-second":{"l":["{0} metre per second","{0} metres per second"],"s":"{0} m/s","n":"{0}m/s"}}},"rel":{"s":{"year":["in {0} yr","{0} yr ago",{"0":"this yr","1":"next yr","-1":"last yr"}],"quarter":["in {0} qtr","{0} qtr ago",{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":["in {0} mo","{0} mo ago",{"0":"this mo","1":"next mo","-1":"last mo"}],"week":["in {0} wk","{0} wk ago",{"0":"this wk","1":"next wk","-1":"last wk"}],"hour":["in {0} hr","{0} hr ago",{"0":"this hour"}],"minute":["in {0} min","{0} min ago",{"0":"this minute"}],"second":["in {0} sec","{0} sec ago",{"0":"now"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"cs":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"dl":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"ds":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"dn":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"]},"dn":{"fld":{"l":["era","year","quarter","month","week","day of the week","day","am/pm","hour","minute","second","time zone"],"s":["era","yr","qtr","mo","wk","day of wk","day","am/pm","hr","min","sec","zone"],"n":0}}}`,
    "en-CA": `{"n":{"Bn":[0,"mor",12,"n",13,"aft",18,"eve",21,"night"],"Bn0":["mor","n"],"a":["a.m.","p.m."]},"dp":[["","{d}","{M}","{M2}-{d2}","{M}-{d}","{M} {d}","{y}","{y} (day: {d})","{y}-{M2}","{y}-{M2}-{d2}","{y}-{M}-{d}","{y}-{M}","{M} {y}","{M} {d}, {y}","{d} {y}","{M1}/{d1}/{y1} {G}","{G} (day: {d})","{G} (month: {M})","{M2}-{d2} {G}","{M}-{d} {G}","{M} {d} {G}","{y} {G}","{y} {G} (day: {d})","{M}/{y} {G}","{M}/{d}/{y} {G}","{M} {y} {G}","{M} {d}, {y} {G}","{E}","{E} {d}","{M} {E}","{E}, {M2}-{d2}","{E}, {M2}-{d}","{E}, {M}-{d2}","{E}, {M}-{d}","{E}, {M} {d}","{E} (month: {M})","{y} {E}","{E} {d} {y}","{y}-{M2} {E}","{E}, {y}-{M2}-{d2}","{E}, {y}-{M2}-{d}","{y}-{M} {E}","{E}, {y}-{M}-{d2}","{E}, {y}-{M}-{d}","{M} {y} {E}","{E}, {M} {d}, {y}","{G} {E}","{E} {d} {G}","{E}, {M2}-{d2} {G}","{E}, {M2}-{d} {G}","{E}, {M}-{d2} {G}","{E}, {M}-{d} {G}","{E}, {M} {d} {G}","{y} {G} {E}","{y} {G} (day: {E} {d})","{M}/{y} {G} {E}","{E}, {M}/{d}/{y} {G}","{M} {y} {G} {E}","{E}, {M} {d}, {y} {G}","{E} {y}","{E} {d} {y} {G}"],"01123424425525525567789:;::<==<==<==6>789:;::<==<==<==?@@ABCACCADDADDADDEFFGHHGHHIJJIJJIJJEFFGHHGHHIJJIJJIJJKLLMNOMPQMRRMRRSRRTUUVWXYZ[]^^]^^]^^TUUVWXYZ[]^^]^^]^^_aaAbcAdeAffAff_ffghhijjijjkllkllkllghhijjijjkllkllkllKLLMNOSPQSRRMRRSRRTUUVWXYZ[]^^]^^]^^mUUVWXYZ[]^^]^^]^^_aaAbc_de_ffAff_ffghhijjijjkllkllkllgnhijjijjkllkllkllKLLMNOMPQMRRMRRMRRTUUVWXYZ[]^^]^^]^^TUUVWXYZ[]^^]^^]^^_aaAbcAdeAffAffAffghhijjijjkllkllkllghhijjijjkllkllkll"],"st":{"d":["{El}, {Ml} {d1}, {y1}","{Ml} {d1}, {y1}","{Ms} {d1}, {y1}","{y1}-{M2}-{d2}"]},"mz":{"41":["NST","NDT","Newfoundland Standard Time","Newfoundland Daylight Time","Newfoundland Time"]},"zs":{"America/St_Johns":"NT"},"rs":"–","num":{"cur":{"USD":[["US dollar","US dollars"],"US$","$"],"JPY":["Japanese yen","JP¥","¥"],"CAD":[["Canadian dollar","Canadian dollars"],"$"]},"unit":{"bit":{"l":["{0} bit","{0} bits"],"s":["{0} bit","{0} bits"],"n":["{0}bit","{0}bits"]},"byte":{"l":["{0} byte","{0} bytes"],"s":["{0} byte","{0} bytes"],"n":"{0}B"},"celsius":{"l":["{0} degree Celsius","{0} degrees Celsius"],"s":"{0} °C","n":"{0}°"},"centimeter":{"l":["{0} centimetre","{0} centimetres"],"s":"{0} cm","n":"{0}cm"},"fahrenheit":{"l":["{0} degree Fahrenheit","{0} degrees Fahrenheit"],"s":"{0} °F","n":"{0}°F"},"fluid-ounce":{"l":["{0} US fluid ounce","{0} US fluid ounces"],"s":"{0} US fl oz","n":"{0}US fl oz"},"gallon":{"l":["{0} US gallon","{0} US gallons"],"s":"{0} US gal","n":"{0}USgal"},"hour":{"l":["{0} hour","{0} hours"],"s":["{0} hr","{0} hrs"],"n":"{0}h"},"kilometer":{"l":["{0} kilometre","{0} kilometres"],"s":"{0} km","n":"{0}km"},"liter":{"l":["{0} litre","{0} litres"],"s":"{0} L","n":"{0}L"},"meter":{"l":["{0} metre","{0} metres"],"s":"{0} m","n":"{0}m"},"microsecond":{"l":["{0} microsecond","{0} microseconds"],"s":["{0} μsec","{0} μsecs"],"n":"{0}μs"},"mile-scandinavian":{"l":["{0} Scandinavian mile","{0} Scandinavian miles"],"s":"{0} smi","n":"{0}smi"},"milliliter":{"l":["{0} millilitre","{0} millilitres"],"s":"{0} mL","n":"{0}mL"},"millimeter":{"l":["{0} millimetre","{0} millimetres"],"s":"{0} mm","n":"{0}mm"},"millisecond":{"l":["{0} millisecond","{0} milliseconds"],"s":["{0} millisec","{0} millisecs"],"n":"{0}ms"},"minute":{"l":["{0} minute","{0} minutes"],"s":["{0} min","{0} mins"],"n":"{0}min"},"month":{"l":["{0} month","{0} months"],"s":["{0} mo","{0} mos"],"n":"{0}m"},"nanosecond":{"l":["{0} nanosecond","{0} nanoseconds"],"s":["{0} nanosec","{0} nanosecs"],"n":"{0}ns"},"pound":{"l":["{0} pound","{0} pounds"],"s":"{0} lb","n":"{0}lb"},"second":{"l":["{0} second","{0} seconds"],"s":["{0} sec","{0} secs"],"n":"{0}s"},"stone":{"l":"{0} stone","s":"{0} st","n":"{0}st"},"kilometer-per-hour":{"l":["{0} kilometre per hour","{0} kilometres per hour"],"s":"{0} km/h","n":"{0}km/h"},"meter-per-second":{"l":["{0} metre per second","{0} metres per second"],"s":"{0} m/s","n":"{0}m/s"},"kilobit-per-second":{"l":["{0} kilobit per second","{0} kilobits per second"],"s":"{0} kb/sec","n":"{0}kb/sec"},"megabit-per-second":{"l":["{0} megabit per second","{0} megabits per second"],"s":"{0} Mb/sec","n":"{0}Mb/sec"},"megabyte-per-second":{"l":["{0} megabyte per second","{0} megabytes per second"],"s":"{0} MB/sec","n":"{0}MB/sec"}}},"rel":{"s":{"year":[["in {0} yr","in {0} yrs"],["{0} yr ago","{0} yrs ago"],{"0":"this yr","1":"next yr","-1":"last yr"}],"quarter":[["in {0} qtr","in {0} qtrs"],["{0} qtr ago","{0} qtrs ago"],{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":[["in {0} mo","in {0} mos"],["{0} mo ago","{0} mos ago"],{"0":"this mo","1":"next mo","-1":"last mo"}],"week":[["in {0} wk","in {0} wks"],["{0} wk ago","{0} wks ago"],{"0":"this wk","1":"next wk","-1":"last wk"}],"hour":[["in {0} hr","in {0} hrs"],["{0} hr ago","{0} hrs ago"],{"0":"this hour"}],"minute":[["in {0} min","in {0} mins"],["{0} min ago","{0} mins ago"],{"0":"this minute"}],"second":[["in {0} sec","in {0} secs"],["{0} sec ago","{0} secs ago"],{"0":"now"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"cs":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"dl":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"ds":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"dn":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"]},"dn":{"fld":{"l":["era","year","quarter","month","week","day of the week","day","a.m./p.m.","hour","minute","second","time zone"],"s":["era","yr","qtr","mo","wk","day of wk","day","a.m./p.m.","hr","min","sec","zone"],"n":0}}}`,
    "en-AU": `{"n":{"Bl":[0,"in the morning",12,"midday",13,"in the afternoon",18,"in the evening",21,"at night"],"Bl0":["in the morning","midday"],"Ms":["Jan","Feb","Mar","Apr","May","June","July","Aug","Sept","Oct","Nov","Dec"],"Ls":["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sept","Oct","Nov","Dec"],"Bs":[0,"in the morning",12,"midday",13,"in the afternoon",18,"in the evening",21,"at night"],"Bs0":["in the morning","midday"],"En":["Su.","M.","Tu.","W.","Th.","F.","Sa."],"cn":["S","M","T","W","T","F","S"],"Bn":[0,"in the morning",12,"midday",13,"in the afternoon",18,"in the evening",21,"at night"],"Bn0":["in the morning","midday"],"a":["am","pm"]},"dp":[["","{d}","{M}","{d}/{M}","{d} {M}","{y}","{y} (day: {d})","{M2}/{y}","{d2}/{M2}/{y}","{d}/{M2}/{y}","{M}/{y}","{d2}/{M}/{y}","{d}/{M}/{y}","{M} {y}","{d} {M} {y}","{d} {y}","{d2}/{M2}/{y1} {G}","{G} (day: {d})","{G} (month: {M})","{d}/{M} {G}","{d} {M} {G}","{y} {G}","{y} {G} (day: {d})","{M}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E}, {d}/{M}","{E} {d} {M}","{E} (month: {M})","{y} {E}","{E} {d} {y}","{M2}/{y} {E}","{E}, {d2}/{M2}/{y}","{E}, {d}/{M2}/{y}","{M}/{y} {E}","{E}, {d2}/{M}/{y}","{E}, {d}/{M}/{y}","{M} {y} {E}","{E} {d} {M} {y}","{G} {E}","{E} {d} {G}","{E}, {d}/{M} {G}","{E} {d} {M} {G}","{y} {G} {E}","{y} {G} (day: {E} {d})","{M}/{y} {G} {E}","{E}, {d2}/{M2}/{y} {G}","{E}, {d}/{M2}/{y} {G}","{E}, {d2}/{M}/{y} {G}","{E}, {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E} {d} {M} {y} {G}","{E}, {d} {M}","{E}, {d} {M} {y}","{E} {y}","{E}, {d} {M} {G}","{E}, {d} {M} {y} {G}","{E} {d} {y} {G}","{c}","{M} {c}","{y} {c}","{M2}/{y} {c}","{M}/{y} {c}","{M} {y} {c}","{G} {c}","{y} {G} {c}","{M}/{y} {G} {c}","{M} {y} {G} {c}"],"011233233244244244566789:;<=>>=>>=>>5?67<<:<<=>>=>>=>>@AABCCBCCBDDBDDBDDEFFGHIGJKLMMLMMLMMEFFGHIGJKLMMLMMLMMNOOPQQPQQPRRPRRSRRTUUVWXYZ[]^^]^^]^^TUUVWXYZ[]^^]^^]^^_aaBbbBbbBccBcc_ccdeefghfijkllkllklldeefghfijkllkllkllNOOPQQSQQSRRPmmSmmTUUVWXYZ[]^^]nn]nnoUUVWXYZ[]^^]nn]nn_aaBbb_bb_ccBpp_ppdeefghfijkqqkqqkqqdrefghfijkqqkqqkqqsOOtQQtQQtRRtmmtmmuUUvWXwZ[x^^xnnxnnuUUvWXwZ[x^^xnnxnnyaaBbbBbbBccBppBppzee{gh{ij|qq|qq|qqzee{gh{ij|qq|qq|qq"],"wt":{"n":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","24":"{E} {h}:{m2} {a}","26":"{E} {h}:{m2}:{s2} {a}","28":"{E} {h}:{m2}:{s} {a}","30":"{E} {h}:{m} {a}","32":"{E} {h}:{m}:{s2} {a}","34":"{E} {h}:{m}:{s} {a}","36":"{E} {h} {a}","42":"{E} {h}:{m2} {a}","44":"{E} {h}:{m2}:{s2} {a}","46":"{E} {h}:{m2}:{s} {a}","48":"{E} {h}:{m} {a}","50":"{E} {h}:{m}:{s2} {a}","52":"{E} {h}:{m}:{s} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"24":"{E} {h2}:{m2}","26":"{E} {h2}:{m2}:{s2}","28":"{E} {h2}:{m2}:{s}","30":"{E} {h2}:{m}","32":"{E} {h2}:{m}:{s2}","34":"{E} {h2}:{m}:{s}","42":"{E} {h}:{m2}","44":"{E} {h}:{m2}:{s2}","46":"{E} {h}:{m2}:{s}","48":"{E} {h}:{m}","50":"{E} {h}:{m}:{s2}","52":"{E} {h}:{m}:{s}"}]]},"st":{"d":["{El}, {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d1}/{M1}/{y2}"]},"mz":{"10":["","","Alaska Standard Time","Alaska Daylight Time","Alaska Time"],"15":["","","Central Standard Time","","Central Standard Time"],"18":["","","Mountain Standard Time","Mountain Daylight Time","Mountain Time"],"22":["","","Central Standard Time","Central Daylight Time","Central Time"],"23":["","","Mountain Standard Time","","Mountain Standard Time"],"25":["","","Eastern Standard Time","Eastern Daylight Time","Eastern Time"],"27":["","","Atlantic Standard Time","Atlantic Daylight Time","Atlantic Time"],"35":["","","Pacific Standard Time","Pacific Daylight Time","Pacific Time"],"42":["AWST","","Australian Western Standard Time","","Australian Western Standard Time"],"45":["AEST","AEDT","Australian Eastern Standard Time","Australian Eastern Daylight Time","Australian Eastern Time"],"47":["NZST","NZDT","New Zealand Standard Time","New Zealand Daylight Time","New Zealand Time"],"100":["ACST","ACDT","Australian Central Standard Time","Australian Central Daylight Time","Australian Central Time"],"101":["AEST","","Australian Eastern Standard Time","","Australian Eastern Standard Time"],"133":["","","Hawaii-Aleutian Standard Time","","Hawaii-Aleutian Standard Time"]},"zs":{"America/Los_Angeles":"Los Angeles Time","America/Denver":"Denver Time","America/Phoenix":"Phoenix Time","America/Chicago":"Chicago Time","America/New_York":"New York Time","America/Anchorage":"Anchorage Time","Pacific/Honolulu":"Honolulu Time","America/Halifax":"Halifax Time","America/St_Johns":"St John’s Time","America/Toronto":"Toronto Time","America/Vancouver":"Vancouver Time","America/Mexico_City":"Mexico City Time","America/Costa_Rica":"Costa Rica Time","Australia/Perth":"AWST","Australia/Adelaide":"ACT","Australia/Brisbane":"AEST","Australia/Sydney":"AET","Australia/Melbourne":"AET","Pacific/Auckland":"NZT"},"num":{"exp":"e","cur":{"USD":[["US dollar","US dollars"],"USD","$"],"EUR":[["euro","euros"],"EUR","€"],"GBP":[["British pound","British pounds"],"GBP","£"],"JPY":["Japanese yen","JPY","¥"],"CNY":["Chinese yuan","CNY","¥"],"INR":[["Indian rupee","Indian rupees"],"INR","₹"],"CAD":[["Canadian dollar","Canadian dollars"],"CAD","$"],"AUD":[["Australian dollar","Australian dollars"],"$"],"NZD":[["New Zealand dollar","New Zealand dollars"],"NZD","$"],"SEK":[["Swedish krona","Swedish kronor"],"SEK","Kr"],"RUB":[["Russian rouble","Russian roubles"],"RUB","₽"],"BRL":[["Brazilian real","Brazilian reals"],"BRL","R$"],"MXN":[["Mexican peso","Mexican pesos"],"MXN","$"],"KRW":["South Korean won","KRW","₩"],"HKD":[["Hong Kong dollar","Hong Kong dollars"],"HKD","$"],"TWD":[["New Taiwan dollar","New Taiwan dollars"],"TWD","$"],"TRY":[["Turkish lira","Turkish lire"],"TRY","₺"],"ILS":[["Israeli shekel","Israeli sheckles"],"ILS","₪"],"PHP":[["Philippine peso","Philippine pesos"],"PHP","₱"],"VND":[["Vietnamese dong","Vietnamese dongs"],"VND","₫"],"EGP":[["Egyptian pound","Egyptian pounds"],"EGP","£"]},"unit":{"centimeter":{"l":["{0} centimetre","{0} centimetres"],"s":"{0} cm","n":"{0}cm"},"fahrenheit":{"l":["{0} degree Fahrenheit","{0} degrees Fahrenheit"],"s":"{0}°F","n":0},"fluid-ounce":{"l":["{0} US fluid ounce","{0} US fluid ounces"],"s":"{0} US fl oz","n":"{0}US fl oz"},"gallon":{"l":["{0} US gallon","{0} US gallons"],"s":"{0} gal US","n":0},"hour":{"l":["{0} hour","{0} hours"],"s":["{0} hr","{0} hrs"],"n":"{0}h"},"kilometer":{"l":["{0} kilometre","{0} kilometres"],"s":"{0} km","n":"{0}km"},"liter":{"l":["{0} litre","{0} litres"],"s":"{0} L","n":"{0}L"},"meter":{"l":["{0} metre","{0} metres"],"s":"{0} m","n":"{0}m"},"mile-scandinavian":{"l":["{0} Scandinavian mile","{0} Scandinavian miles"],"s":"{0} smi","n":"{0}smi"},"milliliter":{"l":["{0} millilitre","{0} millilitres"],"s":"{0} mL","n":"{0}mL"},"millimeter":{"l":["{0} millimetre","{0} millimetres"],"s":"{0} mm","n":"{0}mm"},"minute":{"l":["{0} minute","{0} minutes"],"s":["{0} min","{0} mins"],"n":"{0}m"},"percent":{"l":"{0} per cent","s":"{0}%","n":0},"pound":{"l":["{0} pound","{0} pounds"],"s":"{0} lb","n":"{0}lb"},"second":{"l":["{0} second","{0} seconds"],"s":["{0} sec","{0} secs"],"n":"{0}s"},"stone":{"l":"{0} stone","s":"{0} st","n":"{0}st"},"kilometer-per-hour":{"l":["{0} kilometre per hour","{0} kilometres per hour"],"s":"{0} km/h","n":0},"meter-per-second":{"l":["{0} metre per second","{0} metres per second"],"s":"{0} m/s.","n":"{0}m/s"}},"rngS":""},"rel":{"s":{"year":[["in {0} yr","in {0} yrs"],["{0} yr ago","{0} yrs ago"],{"0":"this yr","1":"next yr","-1":"last yr"}],"quarter":[["in {0} qtr","in {0} qtrs"],["{0} qtr ago","{0} qtrs ago"],{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":["in {0} mo","{0} mo ago",{"0":"this mo","1":"next mo","-1":"last mo"}],"week":[["in {0} wk","in {0} wks"],["{0} wk ago","{0} wks ago"],{"0":"this wk","1":"next wk","-1":"last wk"}],"hour":[["in {0} hr","in {0} hrs"],["{0} hr ago","{0} hrs ago"],{"0":"this hour"}],"minute":[["in {0} min","in {0} mins"],["{0} min ago","{0} mins ago"],{"0":"this minute"}],"second":[["in {0} sec","in {0} secs"],["{0} sec ago","{0} secs ago"],{"0":"now"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"cs":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"dl":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"ds":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"dn":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"]},"dn":{"fld":{"l":["era","year","quarter","month","week","day of the week","day","am/pm","hour","minute","second","time zone"],"s":["era","yr","qtr","mo","wk","day of wk","day","am/pm","hr","min","sec","zone"],"n":0}}}`,
    "en-IN": `{"n":{"Ms":["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sept","Oct","Nov","Dec"],"a":["am","pm"]},"dp":[["","{d}","{M}","{d2}/{M2}","{d}/{M}","{d} {M}","{y}","{y} (day: {d})","{M2}/{y}","{d}/{M}/{y}","{M}/{y}","{M} {y}","{d} {M} {y}","{d} {y}","{d2}/{M2}/{y1} {G}","{G} (day: {d})","{G} (month: {M})","{d2}/{M2} {G}","{d}/{M} {G}","{d} {M} {G}","{y} {G}","{y} {G} (day: {d})","{M}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E}, {d2}/{M2}","{E}, {d}/{M2}","{E}, {d2}/{M}","{E}, {d}/{M}","{E}, {d} {M}","{E} (month: {M})","{y} {E}","{E} {d} {y}","{M2}/{y} {E}","{E}, {d}/{M}/{y}","{M}/{y} {E}","{M} {y} {E}","{E}, {d} {M} {y}","{G} {E}","{E} {d} {G}","{E}, {d2}/{M2} {G}","{E}, {d}/{M2} {G}","{E}, {d2}/{M} {G}","{E}, {d}/{M} {G}","{E}, {d} {M} {G}","{y} {G} {E}","{y} {G} (day: {E} {d})","{M}/{y} {G} {E}","{E}, {d2}/{M2}/{y} {G}","{E}, {d}/{M2}/{y} {G}","{E}, {d2}/{M}/{y} {G}","{E}, {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E}, {d} {M} {y} {G}","{E}, {d} {M}, {y}","{E} {y}","{E} {d} {y} {G}"],"011234244255255255677899:99;<<;<<;<<6=7899:99;<<;<<;<<>??@AB@BB@CC@CC@CCDEEFGHFIJKLLKLLKLLDEEFGHFIJKLLKLLKLLMNNOPQORSOTTOTTUTTVWWXYYZYY[]][]][]]VWWXYYZYY[]][]][]]^__@ab@cd@ee@ee^eefgghijhklmnnmnnmnnfgghijhklmnnmnnmnnMNNOPQURSUTTOTTUTTVWWXYYZYY[]][oo[oopWWXYYZYY[]][oo[oo^__@ab^cd^ee@ee^eefgghijhklmnnmnnmnnfqghijhklmnnmnnmnnMNNOPQORSOTTOTTOTTVWWXYYZYY[]][oo[ooVWWXYYZYY[]][oo[oo^__@ab@cd@ee@ee@eefgghijhklmnnmnnmnnfgghijhklmnnmnnmnn"],"wt":{"l":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}"]]},"st":{"d":["{El}, {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d2}/{M2}/{y2}"]},"mz":{"10":["","","Alaska Standard Time","Alaska Daylight Time","Alaska Time"],"15":["","","Central Standard Time","","Central Standard Time"],"18":["","","Mountain Standard Time","Mountain Daylight Time","Mountain Time"],"22":["","","Central Standard Time","Central Daylight Time","Central Time"],"23":["","","Mountain Standard Time","","Mountain Standard Time"],"25":["","","Eastern Standard Time","Eastern Daylight Time","Eastern Time"],"27":["","","Atlantic Standard Time","Atlantic Daylight Time","Atlantic Time"],"35":["","","Pacific Standard Time","Pacific Daylight Time","Pacific Time"],"61":["IST","","India Standard Time","","India Standard Time"],"65":["GST","","Gulf Standard Time","","Gulf Standard Time"],"133":["","","Hawaii-Aleutian Standard Time","","Hawaii-Aleutian Standard Time"]},"zs":{"America/Los_Angeles":"Los Angeles Time","America/Denver":"Denver Time","America/Phoenix":"Phoenix Time","America/Chicago":"Chicago Time","America/New_York":"New York Time","America/Anchorage":"Anchorage Time","Pacific/Honolulu":"Honolulu Time","America/Halifax":"Halifax Time","America/St_Johns":"St John’s Time","America/Toronto":"Toronto Time","America/Vancouver":"Vancouver Time","America/Mexico_City":"Mexico City Time","America/Costa_Rica":"Costa Rica Time","Asia/Dubai":"GST","Asia/Calcutta":"IST"},"num":{"g2":2,"cs":[[3,"#C","K"],[3,"#C","K"],[5,"#C","L"],[5,"#C","L"],[7,"#C","Cr"],[7,"#C","Cr"],[7,"#C","Cr"],[10,"#C","KCr"],[10,"#C","KCr"],[12,"#C","LCr"],[12,"#C","LCr"],[12,"#C","LCr"]],"cl":[[3,"# C","thousand"],[3,"# C","thousand"],[5,"# C","lakh"],[5,"# C","lakh"],[7,"# C","crore"],[7,"# C","crore"],[7,"# C","crore"],[7,"# C","crore"],[7,"# C","crore"],[12,"# C","lakh crore"],[12,"# C","lakh crore"],[12,"# C","lakh crore"]],"cur":{"JPY":["Japanese yen","JP¥","¥"],"RUB":[["Russian rouble","Russian roubles"],"RUB","₽"]},"unit":{"centimeter":{"l":["{0} centimetre","{0} centimetres"],"s":"{0} cm","n":"{0}cm"},"fahrenheit":{"l":["{0} degree Fahrenheit","{0} degrees Fahrenheit"],"s":"{0}°F","n":0},"fluid-ounce":{"l":["{0} US fluid ounce","{0} US fluid ounces"],"s":"{0} US fl oz","n":"{0}US fl oz"},"gallon":{"l":["{0} US gallon","{0} US gallons"],"s":"{0} US gal","n":"{0}USgal"},"hour":{"l":["{0} hour","{0} hours"],"s":["{0} hr","{0} hrs"],"n":"{0}h"},"kilometer":{"l":["{0} kilometre","{0} kilometres"],"s":"{0} km","n":"{0}km"},"liter":{"l":["{0} litre","{0} litres"],"s":"{0} l","n":"{0}l"},"meter":{"l":["{0} metre","{0} metres"],"s":"{0} m","n":"{0}m"},"mile-scandinavian":{"l":["{0} Scandinavian mile","{0} Scandinavian miles"],"s":"{0} smi","n":"{0}smi"},"milliliter":{"l":["{0} millilitre","{0} millilitres"],"s":"{0} ml","n":"{0}ml"},"millimeter":{"l":["{0} millimetre","{0} millimetres"],"s":"{0} mm","n":"{0}mm"},"minute":{"l":["{0} minute","{0} minutes"],"s":["{0} min","{0} mins"],"n":"{0}m"},"percent":{"l":"{0} per cent","s":"{0}%","n":0},"pound":{"l":["{0} pound","{0} pounds"],"s":"{0} lb","n":"{0}lb"},"second":{"l":["{0} second","{0} seconds"],"s":["{0} sec","{0} secs"],"n":"{0}s"},"stone":{"l":"{0} stone","s":"{0} st","n":"{0}st"},"kilometer-per-hour":{"l":["{0} kilometre per hour","{0} kilometres per hour"],"s":"{0} kph","n":"{0}kph"},"meter-per-second":{"l":["{0} metre per second","{0} metres per second"],"s":"{0} m/s","n":"{0}m/s"}}},"rel":{"s":{"year":["in {0} yr","{0} yr ago",{"0":"this yr","1":"next yr","-1":"last yr"}],"quarter":["in {0} qtr","{0} qtr ago",{"0":"this qtr.","1":"next qtr.","-1":"last qtr."}],"month":["in {0} mo","{0} mo ago",{"0":"this mo","1":"next mo","-1":"last mo"}],"week":["in {0} wk","{0} wk ago",{"0":"this wk","1":"next wk","-1":"last wk"}],"hour":["in {0} hr","{0} hr ago",{"0":"this hour"}],"minute":["in {0} min","{0} min ago",{"0":"this minute"}],"second":["in {0} sec","{0} sec ago",{"0":"now"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"cs":["{0} and {1}","{0}, {1}","{0}, {1}","{0} and {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, and {1}"],"dl":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"ds":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"],"dn":["{0} or {1}","{0}, {1}","{0}, {1}","{0} or {1}"]},"dn":{"fld":{"l":["era","year","quarter","month","week","day of the week","day","am/pm","hour","minute","second","time zone"],"s":["era","yr","qtr","mo","wk","day of wk","day","am/pm","hr","min","sec","zone"],"n":0}}}`,
    "de": `{"n":{"Ml":["Januar","Februar","März","April","Mai","Juni","Juli","August","September","Oktober","November","Dezember"],"Ll":0,"El":["Sonntag","Montag","Dienstag","Mittwoch","Donnerstag","Freitag","Samstag"],"cl":0,"Gl":["v. Chr.","n. Chr."],"Bl":[0,"nachts",5,"morgens",10,"vormittags",12,"mittags",13,"nachmittags",18,"abends"],"Bl0":["nachts","mittags"],"Ms":["Jan.","Feb.","März","Apr.","Mai","Juni","Juli","Aug.","Sept.","Okt.","Nov.","Dez."],"Ls":["Jan","Feb","Mär","Apr","Mai","Jun","Jul","Aug","Sep","Okt","Nov","Dez"],"Es":["So.","Mo.","Di.","Mi.","Do.","Fr.","Sa."],"cs":["So","Mo","Di","Mi","Do","Fr","Sa"],"Gs":["v. Chr.","n. Chr."],"Bs":[0,"nachts",5,"morgens",10,"vorm.",12,"mittags",13,"nachm.",18,"abends"],"Bs0":["nachts","mittags"],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["S","M","D","M","D","F","S"],"cn":0,"Gn":["v. Chr.","n. Chr."],"Bn":[0,"nachts",5,"morgens",10,"vorm.",12,"mittags",13,"nachm.",18,"abends"],"Bn0":["nachts","mittags"],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d}.{M}.","{d2}.{M}.","{d}. {M}","{L}","{y}","{y} (Tag: {d})","{M}/{y}","{d}.{M}.{y}","{M} {y}","{d}. {M} {y}","{y} {d}","{d2}.{M2}.{y1} {G}","{G} (Tag: {d})","{G} (Monat: {M})","{G} {d}.{M}.","{G} {d2}.{M}.","{G} {d}. {M}","{G} (Monat: {L})","{y} {G}","{y} {G} (Tag: {d})","{M2}/{y} {G}","{d2}.{M2}.{y} {G}","{d}.{M2}.{y} {G}","{M}/{y} {G}","{d2}.{M}.{y} {G}","{d}.{M}.{y} {G}","{M} {y} {G}","{d}. {M} {y} {G}","{E}","{E}, {d}.","{M} {E}","{E}, {d}.{M}.","{E}, {d}. {M}","{L} {E}","{E} (Monat: {M})","{y} {E}","{y} {E}, {d}.","{M}/{y} {E}","{E}, {d}.{M}.{y}","{M} {y} {E}","{E}, {d}. {M} {y}","{G} {E}","{G} {E}, {d}.","{G} {E}, {d}.{M}.","{G} {E}, {d}. {M}","{y} {G} {E}","{y} {G} (Tag: {E}, {d}.)","{M2}/{y} {G} {E}","{E}, {d2}.{M2}.{y} {G}","{E}, {d}.{M2}.{y} {G}","{M}/{y} {G} {E}","{E}, {d2}.{M}.{y} {G}","{E}, {d}.{M}.{y} {G}","{M} {y} {G} {E}","{E}, {d}. {M} {y} {G}","{c}","{M} {c}","{c} (Monat: {M})","{L} {c}","{y} {c}","{M}/{y} {c}","{M} {y} {c}","{G} {c}","{y} {G} {c}","{M2}/{y} {G} {c}","{M}/{y} {G} {c}","{M} {y} {G} {c}","{y} {G} {E}, {d}."],"0112332432556552557889::9::;<<;<<;<<7=89::9::;<<;<<;<<>??@AA@BA@CCDCC@CCEFFGHIJKLMNNMNNMNNEFFGHIJKLMNNMNNMNNOPPQRRQRRQSSTSSUSSVWWXYYXYYZ[[Z[[Z[[VWWXYYXYYZ[[Z[[Z[[]^^@__@__@aaDaa]aabccdefghijkkjkkjkkbccdefghijkkjkkjkklPPmRRnRRnSSoSSnSSpWWqYYqYYr[[r[[r[[pWWqYYqYYr[[r[[r[[s^^@__s__saaDaasaatccuefvhiwkkwkkwkktxcuefvhiwkkwkkwkkOPPQRRQRRQSSTSSQSSVWWXYYXYYZ[[Z[[Z[[VWWXYYXYYZ[[Z[[Z[[]^^@__@__@aaDaa@aabccdefghijkkjkkjkkbccdefghijkkjkkjkk"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (Sekunde: {s})","{h} {a} {z} (Sekunde: {s})","{h} {a} (Sekunde: {s1})","{h} {a} {z} (Sekunde: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2} Uhr","{h2} Uhr {z}","{h2} Uhr (Sekunde: {s})","{h2} Uhr {z} (Sekunde: {s})","{h2} Uhr (Sekunde: {s1})","{h2} Uhr {z} (Sekunde: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h} Uhr","{h} Uhr {z}","{s} (Stunde: {h} Uhr)","{h} Uhr {z} (Sekunde: {s})","{h} Uhr (Sekunde: {s1})","{h} Uhr {z} (Sekunde: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"24":"{E} {h}:{m2} {a}","30":"{E} {h}:{m} {a}","42":"{E} {h}:{m2} {a}","48":"{E} {h}:{m} {a}"}],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"30":"{E}, {h2}:{m}","34":"{E}, {h2}:{m}:{s}"}]],"s":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E}, {h} {a}","24":"{E} {h}:{m2} {a}","26":"{E}, {h}:{m2}:{s2} {a}","28":"{E}, {h}:{m2}:{s} {a}","30":"{E} {h}:{m} {a}","32":"{E}, {h}:{m}:{s2} {a}","34":"{E}, {h}:{m}:{s} {a}","36":"{E}, {h} {a}","42":"{E} {h}:{m2} {a}","44":"{E}, {h}:{m2}:{s2} {a}","46":"{E}, {h}:{m2}:{s} {a}","48":"{E} {h}:{m} {a}","50":"{E}, {h}:{m}:{s2} {a}","52":"{E}, {h}:{m}:{s} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"24":"{E}, {h2}:{m2}","26":"{E}, {h2}:{m2}:{s2}","28":"{E}, {h2}:{m2}:{s}","30":"{E}, {h2}:{m}","32":"{E}, {h2}:{m}:{s2}","34":"{E}, {h2}:{m}:{s}","42":"{E}, {h}:{m2}","44":"{E}, {h}:{m2}:{s2}","46":"{E}, {h}:{m2}:{s}","48":"{E}, {h}:{m}","50":"{E}, {h}:{m}:{s2}","52":"{E}, {h}:{m}:{s}"}]],"n":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"24":"{E} {h}:{m2} {a}","30":"{E} {h}:{m} {a}","42":"{E} {h}:{m2} {a}","48":"{E} {h}:{m} {a}"}],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"30":"{E}, {h2}:{m}","34":"{E}, {h2}:{m}:{s}"}]]},"g":{"f":"{1} um {0}","l":"{1} um {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {d1}. {Ml} {y1}","{d1}. {Ml} {y1}","{d2}.{M2}.{y1}","{d2}.{M2}.{y2}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} um {0}","{1} um {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","Koordinierte Weltzeit"],"mz":{"5":["OEZ","OESZ","Osteuropäische Normalzeit","Osteuropäische Sommerzeit","Osteuropäische Zeit"],"6":["MEZ","MESZ","Mitteleuropäische Normalzeit","Mitteleuropäische Sommerzeit","Mitteleuropäische Zeit"],"12":["","","Brasília-Normalzeit","","Brasília-Normalzeit"],"18":["","","Rocky-Mountains-Normalzeit","Rocky-Mountains-Sommerzeit","Rocky-Mountains-Zeit"],"22":["","","Nordamerikanische Zentral-Normalzeit","Nordamerikanische Zentral-Sommerzeit","Nordamerikanische Zentralzeit"],"25":["","","Nordamerikanische Ostküsten-Normalzeit","Nordamerikanische Ostküsten-Sommerzeit","Nordamerikanische Ostküstenzeit"],"35":["","","Nordamerikanische Westküsten-Normalzeit","Nordamerikanische Westküsten-Sommerzeit","Nordamerikanische Westküstenzeit"],"45":["","","Ostaustralische Normalzeit","Ostaustralische Sommerzeit","Ostaustralische Zeit"],"61":["","","Indische Normalzeit","","Indische Normalzeit"],"77":["","","Chinesische Normalzeit","","Chinesische Normalzeit"],"82":["","","Koreanische Normalzeit","","Koreanische Normalzeit"],"90":["","","Japanische Normalzeit","","Japanische Normalzeit"],"111":["","","Moskauer Normalzeit","","Moskauer Normalzeit"],"112":["","","Mittlere Greenwich-Zeit","Britische Sommerzeit","Vereinigtes Königreich (Ortszeit)"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":".","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["# %","-# %"],"c":["# ¤","-# ¤"],"ca":["# ¤","-# ¤"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[null,null,null,[6,"# C","Mio."],[6,"# C","Mio."],[6,"# C","Mio."],[9,"# C","Mrd."],[9,"# C","Mrd."],[9,"# C","Mrd."],[12,"# C","Bio."],[12,"# C","Bio."],[12,"# C","Bio."]],"cl":[[3,"# C","Tausend"],[3,"# C","Tausend"],[3,"# C","Tausend"],[[6,"# C","Million"],[6,"# C","Millionen"]],[6,"# C","Millionen"],[6,"# C","Millionen"],[[9,"# C","Milliarde"],[9,"# C","Milliarden"]],[9,"# C","Milliarden"],[9,"# C","Milliarden"],[[12,"# C","Billion"],[12,"# C","Billionen"]],[12,"# C","Billionen"],[12,"# C","Billionen"]],"cur":{"USD":["US-Dollar"],"EUR":["Euro"],"GBP":[["Britisches Pfund","Britische Pfund"]],"JPY":[["Japanischer Yen","Japanische Yen"]],"CNY":[["Chinesischer Yuan","Renminbi Yuan"]],"INR":[["Indische Rupie","Indische Rupien"]],"CAD":[["Kanadischer Dollar","Kanadische Dollar"]],"AUD":[["Australischer Dollar","Australische Dollar"],"AU$"],"CHF":["Schweizer Franken"],"SEK":[["Schwedische Krone","Schwedische Kronen"]],"PLN":[["Polnischer Złoty","Polnische Złoty"]],"RUB":[["Russischer Rubel","Russische Rubel"]],"BRL":[["Brasilianischer Real","Brasilianische Real"]],"MXN":[["Mexikanischer Peso","Mexikanische Pesos"]],"KRW":[["Südkoreanischer Won","Südkoreanische Won"]],"TWD":[0,0,"NT$"],"THB":[0,"฿"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":["{0} Byte","{0} Byte"],"s":["{0} Byte","{0} Byte"],"n":"{0} B"},"kilobyte":{"l":["{0} Kilobyte","{0} Kilobyte"],"s":"{0} kB","n":0},"megabyte":{"l":["{0} Megabyte","{0} Megabyte"],"s":"{0} MB","n":0},"gigabyte":{"l":["{0} Gigabyte","{0} Gigabyte"],"s":"{0} GB","n":0},"terabyte":{"l":["{0} Terabyte","{0} Terabyte"],"s":"{0} TB","n":0},"percent":{"l":"{0} Prozent","s":"{0} %","n":0},"millisecond":{"l":["{0} Millisekunde","{0} Millisekunden"],"s":"{0} ms","n":["{0}ms","{0} ms"]},"second":{"l":["{0} Sekunde","{0} Sekunden"],"s":"{0} Sek.","n":0},"minute":{"l":["{0} Minute","{0} Minuten"],"s":"{0} Min.","n":0},"hour":{"l":["{0} Stunde","{0} Stunden"],"s":"{0} Std.","n":"{0}h"},"day":{"l":["{0} Tag","{0} Tage"],"s":"{0} Tg.","n":"{0} T"},"week":{"l":["{0} Woche","{0} Wochen"],"s":"{0} Wo.","n":"{0} W"},"month":{"l":["{0} Monat","{0} Monate"],"s":"{0} Mon.","n":"{0} M"},"year":{"l":["{0} Jahr","{0} Jahre"],"s":"{0} J","n":0},"kilometer":{"l":"{0} Kilometer","s":"{0} km","n":0},"kilometer-per-hour":{"l":"{0} Kilometer pro Stunde","s":"{0} km/h","n":0}},"rng":"{0}–{1}","approx":"≈","rngS":""},"rel":{"l":{"year":[["in {0} Jahr","in {0} Jahren"],["vor {0} Jahr","vor {0} Jahren"],{"0":"dieses Jahr","1":"nächstes Jahr","-1":"letztes Jahr"}],"quarter":[["in {0} Quartal","in {0} Quartalen"],["vor {0} Quartal","vor {0} Quartalen"],{"0":"dieses Quartal","1":"nächstes Quartal","-1":"letztes Quartal"}],"month":[["in {0} Monat","in {0} Monaten"],["vor {0} Monat","vor {0} Monaten"],{"0":"diesen Monat","1":"nächsten Monat","-1":"letzten Monat"}],"week":[["in {0} Woche","in {0} Wochen"],["vor {0} Woche","vor {0} Wochen"],{"0":"diese Woche","1":"nächste Woche","-1":"letzte Woche"}],"day":[["in {0} Tag","in {0} Tagen"],["vor {0} Tag","vor {0} Tagen"],{"0":"heute","1":"morgen","2":"übermorgen","-2":"vorgestern","-1":"gestern"}],"hour":[["in {0} Stunde","in {0} Stunden"],["vor {0} Stunde","vor {0} Stunden"],{"0":"in dieser Stunde"}],"minute":[["in {0} Minute","in {0} Minuten"],["vor {0} Minute","vor {0} Minuten"],{"0":"in dieser Minute"}],"second":[["in {0} Sekunde","in {0} Sekunden"],["vor {0} Sekunde","vor {0} Sekunden"],{"0":"jetzt"}]},"s":{"year":[["in {0} Jahr","in {0} Jahren"],["vor {0} Jahr","vor {0} Jahren"],{"0":"dieses Jahr","1":"nächstes Jahr","-1":"letztes Jahr"}],"quarter":["in {0} Quart.","vor {0} Quart.",{"0":"dieses Quartal","1":"nächstes Quartal","-1":"letztes Quartal"}],"month":[["in {0} Monat","in {0} Monaten"],["vor {0} Monat","vor {0} Monaten"],{"0":"diesen Monat","1":"nächsten Monat","-1":"letzten Monat"}],"week":[["in {0} Woche","in {0} Wochen"],["vor {0} Woche","vor {0} Wochen"],{"0":"diese Woche","1":"nächste Woche","-1":"letzte Woche"}],"day":[["in {0} Tag","in {0} Tagen"],["vor {0} Tag","vor {0} Tagen"],{"0":"heute","1":"morgen","2":"übermorgen","-2":"vorgestern","-1":"gestern"}],"hour":["in {0} Std.","vor {0} Std.",{"0":"in dieser Stunde"}],"minute":["in {0} Min.","vor {0} Min.",{"0":"in dieser Minute"}],"second":["in {0} Sek.","vor {0} Sek.",{"0":"jetzt"}]},"n":{"year":0,"quarter":["in {0} Q","vor {0} Q",{"0":"dieses Quartal","1":"nächstes Quartal","-1":"letztes Quartal"}],"month":[["in {0} Monat","in {0} Monaten"],["vor {0} Monat","vor {0} Monaten"],{"0":"diesen Monat","1":"nächsten Monat","-1":"letzten Monat"}],"week":["in {0} Wo.","vor {0} Wo.",{"0":"diese Woche","1":"nächste Woche","-1":"letzte Woche"}],"day":0,"hour":0,"minute":["in {0} m","vor {0} m",{"0":"in dieser Minute"}],"second":["in {0} s","vor {0} s",{"0":"jetzt"}]}},"list":{"cl":["{0} und {1}","{0}, {1}","{0}, {1}","{0} und {1}"],"cs":["{0} und {1}","{0}, {1}","{0}, {1}","{0} und {1}"],"cn":["{0} und {1}","{0}, {1}","{0}, {1}","{0} und {1}"],"dl":["{0} oder {1}","{0}, {1}","{0}, {1}","{0} oder {1}"],"ds":["{0} oder {1}","{0}, {1}","{0}, {1}","{0} oder {1}"],"dn":["{0} oder {1}","{0}, {1}","{0}, {1}","{0} oder {1}"],"ul":["{0}, {1}","{0}, {1}","{0}, {1}","{0} und {1}"],"us":["{0}, {1}","{0}, {1}","{0}, {1}","{0} und {1}"],"un":["{0}, {1}","{0}, {1}","{0}, {1}","{0} und {1}"]},"dn":{"lang":{"de-AT":"Österreichisches Deutsch","de-CH":"Schweizer Hochdeutsch","en-AU":"Englisch (Australien)","en-CA":"Englisch (Kanada)","fr-CH":"Französisch (Schweiz)","nl-BE":"Flämisch","es-ES":"Spanisch (Spanien)","yue":"Kantonesisch","haw":"Hawaiisch","tlh":"Klingonisch","sr":"Serbisch","hr":"Kroatisch","sk":"Slowakisch","sl":"Slowenisch","bg":"Bulgarisch","lt":"Litauisch","lv":"Lettisch","et":"Estnisch","is":"Isländisch","ka":"Georgisch","hy":"Armenisch","az":"Aserbaidschanisch","kk":"Kasachisch","ne":"Nepalesisch","si":"Singhalesisch","km":"Khmer","lo":"Laotisch","my":"Birmanisch","am":"Amharisch","zu":"Zulu","af":"Afrikaans","gl":"Galicisch","eu":"Baskisch","cy":"Walisisch","mt":"Maltesisch","en":"Englisch","es":"Spanisch","fr":"Französisch","de":"Deutsch","it":"Italienisch","pt":"Portugiesisch","nl":"Niederländisch","sv":"Schwedisch","pl":"Polnisch","ru":"Russisch","uk":"Ukrainisch","ja":"Japanisch","zh":"Chinesisch","ko":"Koreanisch","ar":"Arabisch","he":"Hebräisch","hi":"Hindi","bn":"Bengalisch","ur":"Urdu","fa":"Persisch","tr":"Türkisch","el":"Griechisch","cs":"Tschechisch","da":"Dänisch","fi":"Finnisch","nb":"Norwegisch (Bokmål)","no":"Norwegisch","hu":"Ungarisch","ro":"Rumänisch","id":"Indonesisch","ms":"Malaiisch","th":"Thailändisch","vi":"Vietnamesisch","tl":"Filipino","fil":"Filipino","ta":"Tamil","te":"Telugu","sw":"Suaheli","ca":"Katalanisch","eo":"Esperanto","la":"Latein","ga":"Irisch","zh-Hans":"Chinesisch (vereinfacht)","zh-Hant":"Chinesisch (traditionell)","en-US":"Englisch (Vereinigte Staaten)","en-GB":"Englisch (Vereinigtes Königreich)","pt-BR":"Portugiesisch (Brasilien)","pt-PT":"Portugiesisch (Portugal)","es-419":"Spanisch (Lateinamerika)","es-MX":"Spanisch (Mexiko)","fr-CA":"Französisch (Kanada)"},"langS":{"en-US":"Englisch (USA)","en-GB":"Englisch (GB)"},"reg":{"142":"Asien","150":"Europa","419":"Lateinamerika","US":"Vereinigte Staaten","GB":"Vereinigtes Königreich","CA":"Kanada","AU":"Australien","NZ":"Neuseeland","IE":"Irland","IN":"Indien","PK":"Pakistan","BD":"Bangladesch","DE":"Deutschland","AT":"Österreich","CH":"Schweiz","FR":"Frankreich","BE":"Belgien","LU":"Luxemburg","ES":"Spanien","PT":"Portugal","IT":"Italien","NL":"Niederlande","SE":"Schweden","NO":"Norwegen","DK":"Dänemark","FI":"Finnland","IS":"Island","PL":"Polen","CZ":"Tschechien","SK":"Slowakei","HU":"Ungarn","RO":"Rumänien","BG":"Bulgarien","GR":"Griechenland","TR":"Türkei","RU":"Russland","UA":"Ukraine","BY":"Belarus","IL":"Israel","EG":"Ägypten","SA":"Saudi-Arabien","AE":"Vereinigte Arabische Emirate","IR":"Iran","ZA":"Südafrika","NG":"Nigeria","KE":"Kenia","MX":"Mexiko","BR":"Brasilien","AR":"Argentinien","CL":"Chile","CO":"Kolumbien","PE":"Peru","VE":"Venezuela","CU":"Kuba","JP":"Japan","CN":"China","TW":"Taiwan","HK":"Sonderverwaltungsregion Hongkong","KR":"Südkorea","SG":"Singapur","MY":"Malaysia","ID":"Indonesien","TH":"Thailand","VN":"Vietnam","PH":"Philippinen","XK":"Kosovo","RS":"Serbien","HR":"Kroatien","SI":"Slowenien","BA":"Bosnien und Herzegowina","AL":"Albanien","LT":"Litauen","LV":"Lettland","EE":"Estland","GE":"Georgien","KZ":"Kasachstan","EU":"Europäische Union","UN":"Vereinte Nationen","ZZ":"Unbekannte Region","AQ":"Antarktis","001":"Welt","002":"Afrika","019":"Amerika"},"regS":{"US":"USA","GB":"UK","HK":"Hongkong","UN":"UN"},"cur":{"USD":"US-Dollar","EUR":"Euro","GBP":"Britisches Pfund","JPY":"Japanischer Yen","CNY":"Renminbi Yuan","INR":"Indische Rupie","CAD":"Kanadischer Dollar","AUD":"Australischer Dollar","CHF":"Schweizer Franken","BRL":"Brasilianischer Real","MXN":"Mexikanischer Peso","KRW":"Südkoreanischer Won","RUB":"Russischer Rubel","SEK":"Schwedische Krone","PLN":"Polnischer Złoty","XXX":"Unbekannte Währung","HKD":"Hongkong-Dollar","TWD":"Neuer Taiwan-Dollar","NZD":"Neuseeland-Dollar","NOK":"Norwegische Krone","DKK":"Dänische Krone","CZK":"Tschechische Krone","HUF":"Ungarischer Forint","TRY":"Türkische Lira","ZAR":"Südafrikanischer Rand","SGD":"Singapur-Dollar","ILS":"Israelischer Neuer Schekel","AED":"VAE-Dirham"},"scr":{"Latn":"Lateinisch","Cyrl":"Kyrillisch","Arab":"Arabisch","Hans":"Vereinfacht","Hant":"Traditionell","Jpan":"Japanisch","Kore":"Koreanisch","Grek":"Griechisch","Hebr":"Hebräisch","Deva":"Devanagari"},"cal":{"buddhist":"Buddhistischer Kalender","chinese":"Chinesischer Kalender","coptic":"Koptischer Kalender","dangi":"Dangi-Kalender","ethioaa":"Äthiopischer Amätä-Aläm-Kalender","ethiopic":"Äthiopischer Kalender","gregory":"Gregorianischer Kalender","hebrew":"Jüdischer Kalender","indian":"Indischer Nationalkalender","islamic":"Hidschra-Kalender","islamic-civil":"Hidschra-Kalender (tabellarisch, nicht-astronomisch)","islamic-umalqura":"Hidschra-Kalender (Umm al-Qura)","iso8601":"ISO-8601-Kalender","japanese":"Japanischer Kalender","persian":"Persischer Kalender","roc":"Minguo-Kalender"},"fld":{"l":["Epoche","Jahr","Quartal","Monat","Woche","Wochentag","Tag","Tageshälfte","Stunde","Minute","Sekunde","Zeitzone"],"s":["Ep.","Jahr","Quart.","Mon.","Woche","Wochentag","Tag","Tageshälfte","Std.","Min.","Sek.","Zeitzone"],"n":["E","J","Q","M","W","Wochent.","Tag","Tagesh.","Std.","Min.","Sek.","Zeitz."]},"lp":["{0} ({1})",", "]}}`,
    "fr": `{"n":{"Ml":["janvier","février","mars","avril","mai","juin","juillet","août","septembre","octobre","novembre","décembre"],"Ll":0,"El":["dimanche","lundi","mardi","mercredi","jeudi","vendredi","samedi"],"cl":0,"Gl":["avant Jésus-Christ","après Jésus-Christ"],"Bl":[0,"du matin",12,"midi",13,"de l’après-midi",18,"du soir"],"Bl0":["du matin","midi"],"Ms":["janv.","févr.","mars","avr.","mai","juin","juil.","août","sept.","oct.","nov.","déc."],"Ls":0,"Es":["dim.","lun.","mar.","mer.","jeu.","ven.","sam."],"cs":0,"Gs":["av. J.-C.","ap. J.-C."],"Bs":[0,"matin",12,"midi",13,"après-midi",18,"soir"],"Bs0":["matin","midi"],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["D","L","M","M","J","V","S"],"cn":0,"Gn":["av. J.-C.","ap. J.-C."],"Bn":[0,"matin",4,"mat.",12,"midi",13,"ap.m.",18,"soir"],"Bn0":["matin","midi"],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d2}/{M2}","{d}/{M2}","{d2}/{M}","{d}/{M}","{d} {M}","{y}","{y} (jour: {d})","{M2}/{y}","{d2}/{M2}/{y}","{d}/{M}/{y}","{M}/{y}","{M} {y}","{d} {M} {y}","{y} {d}","{d2}/{M2}/{y1} {G}","{G} (jour: {d})","{G} (mois: {M})","{G} {d2}/{M2}","{G} {d}/{M2}","{G} {d2}/{M}","{G} {d}/{M}","{G} {d} {M}","{y} {G}","{y} {G} (jour: {d})","{M2}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{M}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E} {d2}/{M2}","{E} {d}/{M2}","{E} {d2}/{M}","{E} {d}/{M}","{E} {d} {M}","{E} (mois: {M})","{y} {E}","{y} {E} {d}","{M2}/{y} {E}","{E} {d2}/{M2}/{y}","{E} {d}/{M2}/{y}","{M}/{y} {E}","{E} {d2}/{M}/{y}","{E} {d}/{M}/{y}","{M} {y} {E}","{E} {d} {M} {y}","{G} {E}","{G} {E} {d}","{G} {E} {d2}/{M2}","{G} {E} {d}/{M2}","{G} {E} {d2}/{M}","{G} {E} {d}/{M}","{G} {E} {d} {M}","{y} {G} {E}","{y} {G} (jour: {E} {d})","{M2}/{y} {G} {E}","{E} {d2}/{M2}/{y} {G}","{E} {d}/{M2}/{y} {G}","{M}/{y} {G} {E}","{E} {d2}/{M}/{y} {G}","{E} {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E} {d} {M} {y} {G}","{y} {G} {E} {d}"],"011234256277277277899:;<=<<>??>??>??8@9:;<=<<>??>??>??ABBCDECFGCHHCHHCHHIJJKLMNOPQRRQRRQRRIJJKLMNOPQRRQRRQRRSTTUVWUXYUZZUZZ[ZZ]^^_abcdefggfggfgg]^^_abcdefggfggfgghiiCjkClmCnnCnnhnnoppqrstuvwxxwxxwxxoppqrstuvwxxwxxwxxSTTUVW[XY[ZZUZZ[ZZ]^^_abcdefggfggfgg]^^_abcdefggfggfgghiiCjkhlmhnnCnnhnnoppqrstuvwxxwxxwxxoypqrstuvwxxwxxwxxSTTUVWUXYUZZUZZUZZ]^^_abcdefggfggfgg]^^_abcdefggfggfgghiiCjkClmCnnCnnCnnoppqrstuvwxxwxxwxxoppqrstuvwxxwxxwxx"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (seconde: {s})","{h} {a} {z} (seconde: {s})","{h} {a} (seconde: {s1})","{h} {a} {z} (seconde: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2} h","{h2} h {z}","{h2} h (seconde: {s})","{h2} h {z} (seconde: {s})","{h2} h (seconde: {s1})","{h2} h {z} (seconde: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h} h","{h} h {z}","{s} (heure: {h} h)","{h} h {z} (seconde: {s})","{h} h (seconde: {s1})","{h} h {z} (seconde: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1} {0}","{1} {0}","{1} {0}","{1} {0}"],["{1} {0}","{1} {0}","{1} {0}","{1} {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} à {0}","l":"{1} à {0}","m":"{1}, {0}","s":"{1} {0}"},"st":{"d":["{El} {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d2}/{M2}/{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} à {0}","{1} à {0}","{1}, {0}","{1} {0}"]},"hc":"h23","hc12":"h12","gmt":["UTC{0}","UTC+0","+HH:mm","−HH:mm","UTC","temps universel coordonné"],"mz":{"5":["","","heure normale d’Europe de l’Est","heure d’été d’Europe de l’Est","heure d’Europe de l’Est"],"6":["","","heure normale d’Europe centrale","heure d’été d’Europe centrale","heure d’Europe centrale"],"12":["","","heure normale de Brasilia","","heure normale de Brasilia"],"18":["","","heure normale des Rocheuses","heure d’été des Rocheuses","heure des Rocheuses"],"22":["","","heure normale du centre nord-américain","heure d’été du centre nord-américain","heure du centre nord-américain"],"25":["","","heure normale de l’Est nord-américain","heure d’été de l’Est nord-américain","heure de l’Est nord-américain"],"35":["","","heure normale du Pacifique nord-américain","heure d’été du Pacifique nord-américain","heure du Pacifique nord-américain"],"45":["","","heure normale de l’Est de l’Australie","heure d’été de l’Est de l’Australie","heure de l’Est de l’Australie"],"61":["","","heure de l’Inde","","heure de l’Inde"],"77":["","","heure normale de la Chine","","heure normale de la Chine"],"82":["","","heure normale de la Corée","","heure normale de la Corée"],"90":["","","heure normale du Japon","","heure normale du Japon"],"111":["","","heure normale de Moscou","","heure normale de Moscou"],"112":["","","heure moyenne de Greenwich","heure d’été britannique","heure : Royaume-Uni"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":" ","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["# %","-# %"],"c":["# ¤","-# ¤"],"ca":["# ¤","(# ¤)"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","k"],[3,"# C","k"],[3,"# C","k"],[6,"# C","M"],[6,"# C","M"],[6,"# C","M"],[9,"# C","Md"],[9,"# C","Md"],[9,"# C","Md"],[12,"# C","Bn"],[12,"# C","Bn"],[12,"# C","Bn"]],"cl":[{"p":[[3,"# C","millier"],0,[3,"# C","mille"]],"x":[null,"C","mille"]},[3,"# C","mille"],[3,"# C","mille"],[[6,"# C","million"],0,[6,"# C","millions"]],[6,"# C","millions"],[6,"# C","millions"],[[9,"# C","milliard"],0,[9,"# C","milliards"]],[9,"# C","milliards"],[9,"# C","milliards"],[[12,"# C","billion"],0,[12,"# C","billions"]],[12,"# C","billions"],[12,"# C","billions"]],"cur":{"USD":[["dollar des États-Unis","dollars des États-Unis","dollars des États-Unis"],"$US"],"EUR":[["euro","euros","euros"]],"GBP":[["livre sterling","livres sterling","livres sterling"],"£GB"],"JPY":[["yen japonais","yens japonais","yens japonais"],"JPY"],"CNY":[["yuan renminbi chinois","yuans renminbi chinois","yuans renminbi chinois"],"CNY"],"INR":[["roupie indienne","roupies indiennes","roupies indiennes"]],"CAD":[["dollar canadien","dollars canadiens","dollars canadiens"],"$CA"],"AUD":[["dollar australien","dollars australiens","dollars australiens"],"$AU"],"NZD":[0,"$NZ"],"CHF":[["franc suisse","francs suisses","francs suisses"]],"SEK":[["couronne suédoise","couronnes suédoises","couronnes suédoises"]],"PLN":[["zloty polonais","zlotys polonais","zlotys polonais"]],"RUB":[["rouble russe","roubles russes","roubles russes"]],"BRL":[["réal brésilien","réals brésiliens","réals brésiliens"]],"MXN":[["peso mexicain","pesos mexicains","pesos mexicains"],"$MX"],"KRW":[["won sud-coréen","wons sud-coréens","wons sud-coréens"]],"HKD":[0,"HKD"],"SGD":[0,"$SG"],"TWD":[0,"TWD","NT$"],"PHP":[0,"PHP"],"ARS":[0,"$AR"],"CLP":[0,"$CL"],"COP":[0,"$CO"],"EGP":[0,0,"£E"]},"unit":{"byte":{"l":["{0} octet","{0} octets","{0} octets"],"s":"{0} o","n":"{0}o"},"kilobyte":{"l":["{0} kilooctet","{0} kilooctets","{0} kilooctets"],"s":"{0} ko","n":"{0}ko"},"megabyte":{"l":["{0} mégaoctet","{0} mégaoctets","{0} mégaoctets"],"s":"{0} Mo","n":"{0}Mo"},"gigabyte":{"l":["{0} gigaoctet","{0} gigaoctets","{0} gigaoctets"],"s":"{0} Go","n":"{0}Go"},"terabyte":{"l":["{0} téraoctet","{0} téraoctets","{0} téraoctets"],"s":"{0} To","n":"{0}To"},"percent":{"l":"{0} pour cent","s":"{0} %","n":0},"millisecond":{"l":["{0} milliseconde","{0} millisecondes","{0} millisecondes"],"s":"{0} ms","n":"{0}ms"},"second":{"l":["{0} seconde","{0} secondes","{0} secondes"],"s":"{0} s","n":"{0}s"},"minute":{"l":["{0} minute","{0} minutes","{0} minutes"],"s":"{0} min","n":"{0}min"},"hour":{"l":["{0} heure","{0} heures","{0} heures"],"s":"{0} h","n":"{0}h"},"day":{"l":["{0} jour","{0} jours","{0} jours"],"s":"{0} j","n":"{0}j"},"week":{"l":["{0} semaine","{0} semaines","{0} semaines"],"s":"{0} sem.","n":"{0}sem."},"month":{"l":"{0} mois","s":"{0} m.","n":"{0}m."},"year":{"l":["{0} an","{0} ans","{0} ans"],"s":["{0} an","{0} ans","{0} ans"],"n":"{0}a"},"kilometer":{"l":["{0} kilomètre","{0} kilomètres","{0} kilomètres"],"s":"{0} km","n":"{0}km"},"kilometer-per-hour":{"l":["{0} kilomètre par heure","{0} kilomètres par heure","{0} kilomètres par heure"],"s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}–{1}","approx":"≃","rngS":""},"rel":{"l":{"year":[["dans {0} an","dans {0} ans","dans {0} ans"],["il y a {0} an","il y a {0} ans","il y a {0} ans"],{"0":"cette année","1":"l’année prochaine","-1":"l’année dernière"}],"quarter":[["dans {0} trimestre","dans {0} trimestres","dans {0} trimestres"],["il y a {0} trimestre","il y a {0} trimestres","il y a {0} trimestres"],{"0":"ce trimestre","1":"le trimestre prochain","-1":"le trimestre dernier"}],"month":["dans {0} mois","il y a {0} mois",{"0":"ce mois-ci","1":"le mois prochain","-1":"le mois dernier"}],"week":[["dans {0} semaine","dans {0} semaines","dans {0} semaines"],["il y a {0} semaine","il y a {0} semaines","il y a {0} semaines"],{"0":"cette semaine","1":"la semaine prochaine","-1":"la semaine dernière"}],"day":[["dans {0} jour","dans {0} jours","dans {0} jours"],["il y a {0} jour","il y a {0} jours","il y a {0} jours"],{"0":"aujourd’hui","1":"demain","2":"après-demain","-2":"avant-hier","-1":"hier"}],"hour":[["dans {0} heure","dans {0} heures","dans {0} heures"],["il y a {0} heure","il y a {0} heures","il y a {0} heures"],{"0":"cette heure-ci"}],"minute":[["dans {0} minute","dans {0} minutes","dans {0} minutes"],["il y a {0} minute","il y a {0} minutes","il y a {0} minutes"],{"0":"cette minute-ci"}],"second":[["dans {0} seconde","dans {0} secondes","dans {0} secondes"],["il y a {0} seconde","il y a {0} secondes","il y a {0} secondes"],{"0":"maintenant"}]},"s":{"year":["dans {0} a","il y a {0} a",{"0":"cette année","1":"l’an prochain","-1":"l’an dernier"}],"quarter":["dans {0} trim.","il y a {0} trim.",{"0":"ce trimestre","1":"le trimestre prochain","-1":"le trimestre dernier"}],"month":["dans {0} m.","il y a {0} m.",{"0":"ce mois-ci","1":"le mois prochain","-1":"le mois dernier"}],"week":["dans {0} sem.","il y a {0} sem.",{"0":"cette semaine","1":"la semaine prochaine","-1":"la semaine dernière"}],"day":["dans {0} j","il y a {0} j",{"0":"aujourd’hui","1":"demain","2":"après-demain","-2":"avant-hier","-1":"hier"}],"hour":["dans {0} h","il y a {0} h",{"0":"cette heure-ci"}],"minute":["dans {0} min","il y a {0} min",{"0":"cette minute-ci"}],"second":["dans {0} s","il y a {0} s",{"0":"maintenant"}]},"n":{"year":["+{0} a","-{0} a",{"0":"cette année","1":"l’an prochain","-1":"l’an dernier"}],"quarter":["+{0} trim.","-{0} trim.",{"0":"ce trimestre","1":"le trimestre prochain","-1":"le trimestre dernier"}],"month":["+{0} m.","-{0} m.",{"0":"ce mois-ci","1":"le mois prochain","-1":"le mois dernier"}],"week":["+{0} sem.","-{0} sem.",{"0":"cette semaine","1":"la semaine prochaine","-1":"la semaine dernière"}],"day":["+{0} j","-{0} j",{"0":"aujourd’hui","1":"demain","2":"après-demain","-2":"avant-hier","-1":"hier"}],"hour":["+{0} h","-{0} h",{"0":"cette heure-ci"}],"minute":["+{0} min","-{0} min",{"0":"cette minute-ci"}],"second":["+{0} s","-{0} s",{"0":"maintenant"}]}},"list":{"cl":["{0} et {1}","{0}, {1}","{0}, {1}","{0} et {1}"],"cs":["{0} et {1}","{0}, {1}","{0}, {1}","{0} et {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"ds":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"dn":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"ul":["{0} et {1}","{0}, {1}","{0}, {1}","{0} et {1}"],"us":["{0} et {1}","{0}, {1}","{0}, {1}","{0} et {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"lang":{"de-AT":"allemand autrichien","de-CH":"allemand suisse","en-AU":"anglais australien","en-CA":"anglais canadien","fr-CH":"français suisse","nl-BE":"flamand","es-ES":"espagnol d’Espagne","yue":"cantonais","haw":"hawaïen","tlh":"klingon","sr":"serbe","hr":"croate","sk":"slovaque","sl":"slovène","bg":"bulgare","lt":"lituanien","lv":"letton","et":"estonien","is":"islandais","ka":"géorgien","hy":"arménien","az":"azerbaïdjanais","kk":"kazakh","ne":"népalais","si":"cingalais","km":"khmer","lo":"lao","my":"birman","am":"amharique","zu":"zoulou","af":"afrikaans","gl":"galicien","eu":"basque","cy":"gallois","mt":"maltais","en":"anglais","es":"espagnol","fr":"français","de":"allemand","it":"italien","pt":"portugais","nl":"néerlandais","sv":"suédois","pl":"polonais","ru":"russe","uk":"ukrainien","ja":"japonais","zh":"chinois","ko":"coréen","ar":"arabe","he":"hébreu","hi":"hindi","bn":"bengali","ur":"ourdou","fa":"persan","tr":"turc","el":"grec","cs":"tchèque","da":"danois","fi":"finnois","nb":"norvégien bokmål","no":"norvégien","hu":"hongrois","ro":"roumain","id":"indonésien","ms":"malais","th":"thaï","vi":"vietnamien","tl":"filipino","fil":"filipino","ta":"tamoul","te":"télougou","sw":"swahili","ca":"catalan","eo":"espéranto","la":"latin","ga":"irlandais","zh-Hans":"chinois simplifié","zh-Hant":"chinois traditionnel","en-US":"anglais américain","en-GB":"anglais britannique","pt-BR":"portugais brésilien","pt-PT":"portugais européen","es-419":"espagnol d’Amérique latine","es-MX":"espagnol du Mexique","fr-CA":"français canadien"},"langS":{"az":"azéri"},"reg":{"142":"Asie","150":"Europe","419":"Amérique latine","US":"États-Unis","GB":"Royaume-Uni","CA":"Canada","AU":"Australie","NZ":"Nouvelle-Zélande","IE":"Irlande","IN":"Inde","PK":"Pakistan","BD":"Bangladesh","DE":"Allemagne","AT":"Autriche","CH":"Suisse","FR":"France","BE":"Belgique","LU":"Luxembourg","ES":"Espagne","PT":"Portugal","IT":"Italie","NL":"Pays-Bas","SE":"Suède","NO":"Norvège","DK":"Danemark","FI":"Finlande","IS":"Islande","PL":"Pologne","CZ":"Tchéquie","SK":"Slovaquie","HU":"Hongrie","RO":"Roumanie","BG":"Bulgarie","GR":"Grèce","TR":"Turquie","RU":"Russie","UA":"Ukraine","BY":"Biélorussie","IL":"Israël","EG":"Égypte","SA":"Arabie saoudite","AE":"Émirats arabes unis","IR":"Iran","ZA":"Afrique du Sud","NG":"Nigeria","KE":"Kenya","MX":"Mexique","BR":"Brésil","AR":"Argentine","CL":"Chili","CO":"Colombie","PE":"Pérou","VE":"Venezuela","CU":"Cuba","JP":"Japon","CN":"Chine","TW":"Taïwan","HK":"R.A.S. chinoise de Hong Kong","KR":"Corée du Sud","SG":"Singapour","MY":"Malaisie","ID":"Indonésie","TH":"Thaïlande","VN":"Viêt Nam","PH":"Philippines","XK":"Kosovo","RS":"Serbie","HR":"Croatie","SI":"Slovénie","BA":"Bosnie-Herzégovine","AL":"Albanie","LT":"Lituanie","LV":"Lettonie","EE":"Estonie","GE":"Géorgie","KZ":"Kazakhstan","EU":"Union européenne","UN":"Nations Unies","ZZ":"région inconnue","AQ":"Antarctique","001":"Monde","002":"Afrique","019":"Amériques"},"regS":{"US":"É.-U.","GB":"R.-U.","HK":"Hong Kong","UN":"ONU"},"cur":{"USD":"dollar des États-Unis","EUR":"euro","GBP":"livre sterling","JPY":"yen japonais","CNY":"yuan renminbi chinois","INR":"roupie indienne","CAD":"dollar canadien","AUD":"dollar australien","CHF":"franc suisse","BRL":"réal brésilien","MXN":"peso mexicain","KRW":"won sud-coréen","RUB":"rouble russe","SEK":"couronne suédoise","PLN":"zloty polonais","XXX":"devise inconnue ou non valide","HKD":"dollar de Hong Kong","TWD":"nouveau dollar taïwanais","NZD":"dollar néo-zélandais","NOK":"couronne norvégienne","DKK":"couronne danoise","CZK":"couronne tchèque","HUF":"forint hongrois","TRY":"livre turque","ZAR":"rand sud-africain","SGD":"dollar de Singapour","ILS":"nouveau shekel israélien","AED":"dirham des Émirats arabes unis"},"scr":{"Latn":"latin","Cyrl":"cyrillique","Arab":"arabe","Hans":"simplifié","Hant":"traditionnel","Jpan":"japonais","Kore":"coréen","Grek":"grec","Hebr":"hébreu","Deva":"dévanagari"},"cal":{"buddhist":"calendrier bouddhiste","chinese":"calendrier chinois","coptic":"calendrier copte","dangi":"calendrier dangi","ethioaa":"calendrier éthiopien Amete Alem","ethiopic":"calendrier éthiopien","gregory":"calendrier grégorien","hebrew":"calendrier hébraïque","indian":"calendrier indien","islamic":"calendrier hégirien","islamic-civil":"calendrier hégirien (tabulaire, époque civile)","islamic-tbla":"calendrier hégirien (tabulaire, époque astronomique)","islamic-umalqura":"calendrier hégirien (Umm al-Qura)","iso8601":"calendrier ISO 8601","japanese":"calendrier japonais","persian":"calendrier persan","roc":"calendrier républicain chinois"},"fld":{"l":["ère","année","trimestre","mois","semaine","jour de la semaine","jour","cadran","heure","minute","seconde","fuseau horaire"],"s":["ère","an","trim.","m.","sem.","j (sem.)","j","cadran","h","min","s","fuseau horaire"],"n":["ère","a","trim.","m.","sem.","j (sem.)","j","cadran","h","min","s","fuseau horaire"]},"lp":["{0} ({1})",", "]}}`,
    "es": `{"n":{"Ml":["enero","febrero","marzo","abril","mayo","junio","julio","agosto","septiembre","octubre","noviembre","diciembre"],"Ll":0,"El":["domingo","lunes","martes","miércoles","jueves","viernes","sábado"],"cl":0,"Gl":["antes de Cristo","después de Cristo"],"Bl":[0,"de la madrugada",6,"de la mañana",12,"del mediodía",13,"de la tarde",20,"de la noche"],"Bl0":["de la madrugada","del mediodía"],"Ms":["ene","feb","mar","abr","may","jun","jul","ago","sept","oct","nov","dic"],"Ls":0,"Es":["dom","lun","mar","mié","jue","vie","sáb"],"cs":0,"Gs":["a. C.","d. C."],"Bs":[0,"de la madrugada",6,"de la mañana",12,"del mediodía",13,"de la tarde",20,"de la noche"],"Bs0":["de la madrugada","del mediodía"],"Mn":["E","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["D","L","M","X","J","V","S"],"cn":0,"Gn":["a. C.","d. C."],"Bn":[0,"de la madrugada",6,"de la mañana",12,"del mediodía",13,"de la tarde",20,"de la noche"],"Bn0":["de la madrugada","del mediodía"],"a":["a. m.","p. m."]},"dp":[["","{d}","{M}","{d}/{M}","{d1}/{M}","{d}/{M1}","{d1}/{M1}","{d} de {M}","{d} {M}","{y}","{y} (día: {d})","{M}/{y}","{d}/{M}/{y}","{M1}/{y}","{M} de {y}","{d} de {M} de {y}","{M} {y}","{d} {M} {y}","{y} {d}","{d1}/{M1}/{y1} {G}","{G} (día: {d})","{G} (mes: {M})","{G} {d}/{M}","{G} {d1}/{M}","{G} {d}/{M1}","{G} {d1}/{M1}","{G} {d} de {M}","{G} {d} {M}","{y} {G}","{y} {G} (día: {d})","{M}/{y} {G}","{d}/{M}/{y} {G}","{M} de {y} {G}","{d} de {M} de {y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E}, {d}/{M}","{E}, {d} de {M}","{E}, {d} {M}","{E} (mes: {M})","{y} {E}","{y} {E} {d}","{M}/{y} {E}","{E}, {d}/{M}/{y}","{M1}/{y} {E}","{M} de {y} {E}","{E}, {d} de {M} de {y}","{M} {y} {E}","{G} {E}","{G} {E} {d}","{G} {E}, {d}/{M}","{G} {E}, {d} de {M}","{G} {E}, {d} {M}","{y} {G} {E}","{y} {G} (día: {E} {d})","{M}/{y} {G} {E}","{E}, {d}/{M}/{y} {G}","{M} de {y} {G} {E}","{E}, {d} de {M} de {y} {G}","{M} {y} {G} {E}","{E}, {d} {M} {y} {G}","{E}, {d} {M} {y}","{y} {G} {E} {d}"],"0112342562772882889::;<<=<<>??@AA@AA9B:;<<=<<>??@AA@AACDDEFGEHIEJJEKKEKKLMMNOONOOPQQRSSRSSLMMNOONOOPQQRSSRSSTUUVWWVWWVXXVYYZYY[]]^__a__bccdccdcc[]]^__a__bccdccdcceffEggEggEhhEiieiijkklmmlmmnoopqqpqqjkklmmlmmnoopqqpqqTUUVWWZWWZXXVYYZYY[]]^__a__bccdrrdrr[]]^__a__bccdrrdrreffEggeggehhEiieiijkklmmlmmnoopqqpqqjsklmmlmmnoopqqpqqTUUVWWVWWVXXVYYVYY[]]^__a__bccdrrdrr[]]^__a__bccdrrdrreffEggEggEhhEiiEiijkklmmlmmnoopqqpqqjkklmmlmmnoopqqpqq"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (segundo: {s})","{h} {a} {z} (segundo: {s})","{h} {a} (segundo: {s1})","{h} {a} {z} (segundo: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h}","{h} {z}","{h} (segundo: {s})","{h} {z} (segundo: {s})","{h} (segundo: {s1})","{h} {z} (segundo: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h}:{m} {z}","{h}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{s} (hora: {h})"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABSDEFGHIJKLMNOPQR"],"wt":{"l":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}"]],"s":0,"n":0},"g":{"f":"{1}, {0}","l":"{1} a las {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {d1} de {Ml} de {y1}","{d1} de {Ml} de {y1}","{d1} {Ms} {y1}","{d1}/{M1}/{y2}"],"t12":["{h1}:{m2}:{s2} {a} {zl}","{h1}:{m2}:{s2} {a} {zs}","{h1}:{m2}:{s2} {a}","{h1}:{m2} {a}"],"t23":["{h1}:{m2}:{s2} ({zl})","{h1}:{m2}:{s2} {zs}","{h1}:{m2}:{s2}","{h1}:{m2}"],"g":["{1}, {0}","{1} a las {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","tiempo universal coordinado"],"mz":{"5":["EET","EEST","hora estándar de Europa oriental","hora de verano de Europa oriental","hora de Europa oriental"],"6":["CET","CEST","hora estándar de Europa central","hora de verano de Europa central","hora de Europa central"],"12":["","","hora estándar de Brasilia","","hora estándar de Brasilia"],"18":["","","hora estándar de las Montañas Rocosas","hora de verano de las Montañas Rocosas","hora de las Montañas Rocosas"],"22":["","","hora estándar central","hora de verano central","hora central"],"25":["","","hora estándar oriental","hora de verano oriental","hora oriental"],"35":["","","hora estándar del Pacífico","hora de verano del Pacífico","hora del Pacífico"],"45":["","","hora estándar de Australia oriental","hora de verano de Australia oriental","hora de Australia oriental"],"61":["","","hora estándar de la India","","hora estándar de la India"],"77":["","","hora estándar de China","","hora estándar de China"],"82":["","","hora estándar de Corea","","hora estándar de Corea"],"90":["","","hora estándar de Japón","","hora estándar de Japón"],"111":["","","hora estándar de Moscú","","hora estándar de Moscú"],"112":["GMT","","hora del meridiano de Greenwich","hora de verano británica","hora de Reino Unido"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":".","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":2,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["# %","-# %"],"c":["# ¤","-# ¤"],"ca":["# ¤","-# ¤"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","mil"],[3,"# C","mil"],[3,"# C","mil"],[6,"# C","M"],[6,"# C","M"],[6,"# C","M"],[6,"# C","M"],[9,"# C","mil M"],[9,"# C","mil M"],[12,"# C","B"],[12,"# C","B"],[12,"# C","B"]],"cl":[[3,"# C","mil"],[3,"# C","mil"],[3,"# C","mil"],[[6,"# C","millón"],0,[6,"# C","millones"]],[6,"# C","millones"],[6,"# C","millones"],[9,"# C","mil millones"],[9,"# C","mil millones"],[9,"# C","mil millones"],[[12,"# C","billón"],0,[12,"# C","billones"]],[12,"# C","billones"],[12,"# C","billones"]],"cur":{"USD":[["dólar estadounidense","dólares estadounidenses","dólares estadounidenses"],"US$"],"EUR":[["euro","euros","euros"]],"GBP":[["libra esterlina","libras esterlinas","libras esterlinas"],"GBP"],"JPY":[["yen japonés","yenes japoneses","yenes japoneses"],"JPY"],"CNY":[["yuan renminbi","yuanes renminbi","yuanes renminbi"],"CNY"],"INR":[["rupia india","rupias indias","rupias indias"],"INR"],"CAD":[["dólar canadiense","dólares canadienses","dólares canadienses"],"CAD"],"AUD":[["dólar australiano","dólares australianos","dólares australianos"],"AUD"],"NZD":[0,"NZD"],"CHF":[["franco suizo","francos suizos","francos suizos"]],"SEK":[["corona sueca","coronas suecas","coronas suecas"]],"PLN":[["esloti polaco","eslotis polacos","eslotis polacos"]],"RUB":[["rublo ruso","rublos rusos","rublos rusos"]],"BRL":[["real brasileño","reales brasileños","reales brasileños"],"BRL"],"MXN":[["peso mexicano","pesos mexicanos","pesos mexicanos"],"MXN"],"KRW":[["won surcoreano","wons surcoreanos","wons surcoreanos"],"KRW"],"HKD":[0,"HKD"],"TWD":[0,"TWD","NT$"],"ILS":[0,"ILS"],"THB":[0,"฿"],"PHP":[0,"PHP"],"EGP":[0,0,"EGP"]},"unit":{"byte":{"l":["{0} byte","{0} bytes","{0} bytes"],"s":"{0} B","n":"{0}B"},"kilobyte":{"l":["{0} kilobyte","{0} kilobytes","{0} kilobytes"],"s":"{0} kB","n":"{0}kB"},"megabyte":{"l":["{0} megabyte","{0} megabytes","{0} megabytes"],"s":"{0} MB","n":"{0}MB"},"gigabyte":{"l":["{0} gigabyte","{0} gigabytes","{0} gigabytes"],"s":"{0} GB","n":"{0}GB"},"terabyte":{"l":["{0} terabyte","{0} terabytes","{0} terabytes"],"s":"{0} TB","n":"{0}TB"},"percent":{"l":"{0} por ciento","s":"{0} %","n":0},"millisecond":{"l":["{0} milisegundo","{0} milisegundos","{0} milisegundos"],"s":"{0} ms","n":"{0}ms"},"second":{"l":["{0} segundo","{0} segundos","{0} segundos"],"s":"{0} s","n":"{0}s"},"minute":{"l":["{0} minuto","{0} minutos","{0} minutos"],"s":"{0} min","n":"{0}min"},"hour":{"l":["{0} hora","{0} horas","{0} horas"],"s":"{0} h","n":"{0}h"},"day":{"l":["{0} día","{0} días","{0} días"],"s":"{0} d","n":"{0}d"},"week":{"l":["{0} semana","{0} semanas","{0} semanas"],"s":"{0} sem.","n":"{0}sem"},"month":{"l":["{0} mes","{0} meses","{0} meses"],"s":"{0} m.","n":"{0}m"},"year":{"l":["{0} año","{0} años","{0} años"],"s":"{0} a","n":"{0}a"},"kilometer":{"l":["{0} kilómetro","{0} kilómetros","{0} kilómetros"],"s":"{0} km","n":"{0}km"},"kilometer-per-hour":{"l":["{0} kilómetro por hora","{0} kilómetros por hora","{0} kilómetros por hora"],"s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}-{1}","approx":"~","rngS":""},"rel":{"l":{"year":[["dentro de {0} año","dentro de {0} años","dentro de {0} años"],["hace {0} año","hace {0} años","hace {0} años"],{"0":"este año","1":"el próximo año","-1":"el año pasado"}],"quarter":[["dentro de {0} trimestre","dentro de {0} trimestres","dentro de {0} trimestres"],["hace {0} trimestre","hace {0} trimestres","hace {0} trimestres"],{"0":"este trimestre","1":"el próximo trimestre","-1":"el trimestre pasado"}],"month":[["dentro de {0} mes","dentro de {0} meses","dentro de {0} meses"],["hace {0} mes","hace {0} meses","hace {0} meses"],{"0":"este mes","1":"el próximo mes","-1":"el mes pasado"}],"week":[["dentro de {0} semana","dentro de {0} semanas","dentro de {0} semanas"],["hace {0} semana","hace {0} semanas","hace {0} semanas"],{"0":"esta semana","1":"la próxima semana","-1":"la semana pasada"}],"day":[["dentro de {0} día","dentro de {0} días","dentro de {0} días"],["hace {0} día","hace {0} días","hace {0} días"],{"0":"hoy","1":"mañana","2":"pasado mañana","-2":"anteayer","-1":"ayer"}],"hour":[["dentro de {0} hora","dentro de {0} horas","dentro de {0} horas"],["hace {0} hora","hace {0} horas","hace {0} horas"],{"0":"esta hora"}],"minute":[["dentro de {0} minuto","dentro de {0} minutos","dentro de {0} minutos"],["hace {0} minuto","hace {0} minutos","hace {0} minutos"],{"0":"este minuto"}],"second":[["dentro de {0} segundo","dentro de {0} segundos","dentro de {0} segundos"],["hace {0} segundo","hace {0} segundos","hace {0} segundos"],{"0":"ahora"}]},"s":{"year":["dentro de {0} a","hace {0} a",{"0":"este año","1":"el próximo año","-1":"el año pasado"}],"quarter":["dentro de {0} trim.","hace {0} trim.",{"0":"este trimestre","1":"el próximo trimestre","-1":"el trimestre pasado"}],"month":["dentro de {0} m","hace {0} m",{"0":"este mes","1":"el próximo mes","-1":"el mes pasado"}],"week":["dentro de {0} sem.","hace {0} sem.",{"0":"esta sem.","1":"próx. sem.","-1":"sem. ant."}],"day":["dentro de {0} d","hace {0} d",{"0":"hoy","1":"mañana","2":"pasado mañana","-2":"anteayer","-1":"ayer"}],"hour":["dentro de {0} h","hace {0} h",{"0":"esta hora"}],"minute":["dentro de {0} min","hace {0} min",{"0":"este minuto"}],"second":["dentro de {0} s","hace {0} s",{"0":"ahora"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} y {1}","{0}, {1}","{0}, {1}","{0} y {1}"],"cs":["{0} y {1}","{0}, {1}","{0}, {1}","{0} y {1}"],"cn":["{0} y {1}","{0}, {1}","{0}, {1}","{0} y {1}"],"dl":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"ds":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"dn":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"ul":["{0} y {1}","{0}, {1}","{0}, {1}","{0} y {1}"],"us":["{0} y {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"lang":{"de-AT":"alemán austríaco","de-CH":"alto alemán suizo","en-AU":"inglés australiano","en-CA":"inglés canadiense","fr-CH":"francés suizo","nl-BE":"flamenco","es-ES":"español de España","yue":"cantonés","haw":"hawaiano","tlh":"klingon","sr":"serbio","hr":"croata","sk":"eslovaco","sl":"esloveno","bg":"búlgaro","lt":"lituano","lv":"letón","et":"estonio","is":"islandés","ka":"georgiano","hy":"armenio","az":"azerbaiyano","kk":"kazajo","ne":"nepalí","si":"cingalés","km":"jemer","lo":"lao","my":"birmano","am":"amárico","zu":"zulú","af":"afrikáans","gl":"gallego","eu":"euskera","cy":"galés","mt":"maltés","en":"inglés","es":"español","fr":"francés","de":"alemán","it":"italiano","pt":"portugués","nl":"neerlandés","sv":"sueco","pl":"polaco","ru":"ruso","uk":"ucraniano","ja":"japonés","zh":"chino","ko":"coreano","ar":"árabe","he":"hebreo","hi":"hindi","bn":"bengalí","ur":"urdu","fa":"persa","tr":"turco","el":"griego","cs":"checo","da":"danés","fi":"finés","nb":"noruego bokmal","no":"noruego","hu":"húngaro","ro":"rumano","id":"indonesio","ms":"malayo","th":"tailandés","vi":"vietnamita","tl":"filipino","fil":"filipino","ta":"tamil","te":"telugu","sw":"suajili","ca":"catalán","eo":"esperanto","la":"latín","ga":"irlandés","zh-Hans":"chino simplificado","zh-Hant":"chino tradicional","en-US":"inglés estadounidense","en-GB":"inglés británico","pt-BR":"portugués de Brasil","pt-PT":"portugués de Portugal","es-419":"español latinoamericano","es-MX":"español de México","fr-CA":"francés canadiense"},"langS":{"az":"azerí","en-US":"inglés (EE. UU.)","en-GB":"inglés (RU)"},"reg":{"142":"Asia","150":"Europa","419":"Latinoamérica","US":"Estados Unidos","GB":"Reino Unido","CA":"Canadá","AU":"Australia","NZ":"Nueva Zelanda","IE":"Irlanda","IN":"India","PK":"Pakistán","BD":"Bangladés","DE":"Alemania","AT":"Austria","CH":"Suiza","FR":"Francia","BE":"Bélgica","LU":"Luxemburgo","ES":"España","PT":"Portugal","IT":"Italia","NL":"Países Bajos","SE":"Suecia","NO":"Noruega","DK":"Dinamarca","FI":"Finlandia","IS":"Islandia","PL":"Polonia","CZ":"Chequia","SK":"Eslovaquia","HU":"Hungría","RO":"Rumanía","BG":"Bulgaria","GR":"Grecia","TR":"Turquía","RU":"Rusia","UA":"Ucrania","BY":"Bielorrusia","IL":"Israel","EG":"Egipto","SA":"Arabia Saudí","AE":"Emiratos Árabes Unidos","IR":"Irán","ZA":"Sudáfrica","NG":"Nigeria","KE":"Kenia","MX":"México","BR":"Brasil","AR":"Argentina","CL":"Chile","CO":"Colombia","PE":"Perú","VE":"Venezuela","CU":"Cuba","JP":"Japón","CN":"China","TW":"Taiwán","HK":"RAE de Hong Kong (China)","KR":"Corea del Sur","SG":"Singapur","MY":"Malasia","ID":"Indonesia","TH":"Tailandia","VN":"Vietnam","PH":"Filipinas","XK":"Kosovo","RS":"Serbia","HR":"Croacia","SI":"Eslovenia","BA":"Bosnia y Herzegovina","AL":"Albania","LT":"Lituania","LV":"Letonia","EE":"Estonia","GE":"Georgia","KZ":"Kazajistán","EU":"Unión Europea","UN":"Naciones Unidas","ZZ":"Región desconocida","AQ":"Antártida","001":"Mundo","002":"África","019":"América"},"regS":{"US":"EE. UU.","GB":"RU","HK":"Hong Kong"},"cur":{"USD":"dólar estadounidense","EUR":"euro","GBP":"libra esterlina","JPY":"yen japonés","CNY":"yuan renminbi","INR":"rupia india","CAD":"dólar canadiense","AUD":"dólar australiano","CHF":"franco suizo","BRL":"real brasileño","MXN":"peso mexicano","KRW":"won surcoreano","RUB":"rublo ruso","SEK":"corona sueca","PLN":"esloti polaco","XXX":"moneda desconocida","HKD":"dólar hongkonés","TWD":"nuevo dólar taiwanés","NZD":"dólar neozelandés","NOK":"corona noruega","DKK":"corona danesa","CZK":"corona checa","HUF":"forinto húngaro","TRY":"lira turca","ZAR":"rand sudafricano","SGD":"dólar singapurense","ILS":"nuevo séquel israelí","AED":"dírham de los Emiratos Árabes Unidos"},"scr":{"Latn":"latino","Cyrl":"cirílico","Arab":"árabe","Hans":"simplificado","Hant":"tradicional","Jpan":"japonés","Kore":"coreano","Grek":"griego","Hebr":"hebreo","Deva":"devanagari"},"cal":{"buddhist":"calendario budista","chinese":"calendario chino","coptic":"calendario cóptico","dangi":"calendario dangi","ethioaa":"calendario etíope Amete Alem","ethiopic":"calendario etíope","gregory":"calendario gregoriano","hebrew":"calendario hebreo","indian":"calendario nacional hindú","islamic":"calendario hijri","islamic-civil":"calendario hijri tabular","islamic-umalqura":"calendario hijri Umm al-Qura","iso8601":"calendario ISO-8601","japanese":"calendario japonés","persian":"calendario persa","roc":"calendario de la República de China"},"fld":{"l":["era","año","trimestre","mes","semana","día de la semana","día","a. m./p. m.","hora","minuto","segundo","zona horaria"],"s":["era","a","trim.","m","sem.","día de sem.","d","a. m./p. m.","h","min","s","zona"],"n":0},"lp":["{0} ({1})",", "]}}`,
    "it": `{"n":{"Ml":["gennaio","febbraio","marzo","aprile","maggio","giugno","luglio","agosto","settembre","ottobre","novembre","dicembre"],"Ll":0,"El":["domenica","lunedì","martedì","mercoledì","giovedì","venerdì","sabato"],"cl":0,"Gl":["avanti Cristo","dopo Cristo"],"Bl":[0,"di notte",6,"di mattina",12,"mezzogiorno",13,"del pomeriggio",18,"di sera"],"Bl0":["di notte","mezzogiorno"],"Ms":["gen","feb","mar","apr","mag","giu","lug","ago","set","ott","nov","dic"],"Ls":0,"Es":["dom","lun","mar","mer","gio","ven","sab"],"cs":0,"Gs":["a.C.","d.C."],"Bs":[0,"di notte",6,"di mattina",12,"mezzogiorno",13,"di pomeriggio",18,"di sera"],"Bs0":["di notte","mezzogiorno"],"Mn":["G","F","M","A","M","G","L","A","S","O","N","D"],"Ln":0,"En":["D","L","M","M","G","V","S"],"cn":0,"Gn":["aC","dC"],"Bn":[0,"di notte",6,"di mattina",12,"mezzogiorno",13,"di pomeriggio",18,"di sera"],"Bn0":["di notte","mezzogiorno"],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d2}/{M2}","{d}/{M2}","{d2}/{M}","{d}/{M}","{d} {M}","{y}","{y} (giorno: {d})","{M2}/{y}","{d2}/{M2}/{y}","{d}/{M2}/{y}","{M}/{y}","{d2}/{M}/{y}","{d}/{M}/{y}","{M} {y}","{d} {M} {y}","{y} {d}","{d2}/{M2}/{y1} {G}","{G} (giorno: {d})","{G} (mese: {M})","{G} {d2}/{M2}","{G} {d}/{M2}","{G} {d2}/{M}","{G} {d}/{M}","{G} {d} {M}","{y} {G}","{y} {G} (giorno: {d})","{M}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E} {d2}/{M2}","{E} {d}/{M2}","{E} {d2}/{M}","{E} {d}/{M}","{E} {d} {M}","{E} (mese: {M})","{y} {E}","{y} {E} {d}","{M2}/{y} {E}","{E} {d2}/{M2}/{y}","{E} {d}/{M2}/{y}","{M}/{y} {E}","{E} {d2}/{M}/{y}","{E} {d}/{M}/{y}","{M} {y} {E}","{E} {d} {M} {y}","{G} {E}","{G} {E} {d}","{G} {E} {d2}/{M2}","{G} {E} {d}/{M2}","{G} {E} {d2}/{M}","{G} {E} {d}/{M}","{G} {E} {d} {M}","{y} {G} {E}","{y} {G} (giorno: {E} {d})","{M}/{y} {G} {E}","{E} {d2}/{M2}/{y} {G}","{E} {d}/{M2}/{y} {G}","{E} {d2}/{M}/{y} {G}","{E} {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E} {d} {M} {y} {G}","{y} {G} {E} {d}"],"011234256277277277899:;<=>?@AA@AA@AA8B9:;?=??@AA@AA@AACDDEFGEHIEJJEJJEJJKLLMNOMPQRSSRSSRSSKLLMNOMPQRSSRSSRSSTUUVWXVYZV[[V[[][[^__abcdefghhghhghh^__abcdefghhghhghhijjEklEmnEooEooioopqqrstruvwxxwxxwxxpqqrstruvwxxwxxwxxTUUVWX]YZ][[V[[][[^__abcdefghhghhghh^__abcdefghhghhghhijjEklimniooEooioopqqrstruvwxxwxxwxxpyqrstruvwxxwxxwxxTUUVWXVYZV[[V[[V[[^__abcdefghhghhghh^__abcdefghhghhghhijjEklEmnEooEooEoopqqrstruvwxxwxxwxxpqqrstruvwxxwxxwxx"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (secondo: {s})","{h} {a} {z} (secondo: {s})","{h} {a} (secondo: {s1})","{h} {a} {z} (secondo: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h}","{h2} {z}","{h} (secondo: {s})","{h2} {z} (secondo: {s})","{h} (secondo: {s1})","{h2} {z} (secondo: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h} {z}","{s} (ora: {h})","{h} {z} (secondo: {s})","{h} {z} (secondo: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRASTUEVWXYJZLM[]PQR"],"wt":{"l":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} alle ore {0}","l":"{1} alle ore {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El} {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d2}/{M2}/{y2}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} alle ore {0}","{1} alle ore {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","Tempo coordinato universale"],"mz":{"5":["EET","EEST","Ora standard dell’Europa orientale","Ora legale dell’Europa orientale","Ora dell’Europa orientale"],"6":["CET","CEST","Ora standard dell’Europa centrale","Ora legale dell’Europa centrale","Ora dell’Europa centrale"],"12":["","","Ora standard di Brasilia","","Ora standard di Brasilia"],"18":["","","Ora standard Montagne Rocciose USA","Ora legale Montagne Rocciose USA","Ora Montagne Rocciose USA"],"22":["","","Ora standard centrale USA","Ora legale centrale USA","Ora centrale USA"],"25":["","","Ora standard orientale USA","Ora legale orientale USA","Ora orientale USA"],"35":["","","Ora standard del Pacifico USA","Ora legale del Pacifico USA","Ora del Pacifico USA"],"45":["","","Ora standard dell’Australia orientale","Ora legale dell’Australia orientale","Ora dell’Australia orientale"],"61":["","","Ora standard dell’India","","Ora standard dell’India"],"77":["","","Ora standard della Cina","","Ora standard della Cina"],"82":["","","Ora standard coreana","","Ora standard coreana"],"90":["","","Ora standard del Giappone","","Ora standard del Giappone"],"111":["","","Ora standard di Mosca","","Ora standard di Mosca"],"112":["","","Ora del meridiano di Greenwich","Ora legale del Regno Unito","Ora Regno Unito"]},"zg":{},"rs":"6 – 0","fs":",","num":{"dec":",","grp":".","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":2,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["# ¤","-# ¤"],"ca":["# ¤","-# ¤"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"#C","K"],[3,"#C","K"],[3,"#C","K"],[6,"# C","Mln"],[6,"# C","Mln"],[6,"# C","Mln"],[9,"# C","Mld"],[9,"# C","Mld"],[9,"# C","Mld"],[12,"# C","Bln"],[12,"# C","Bln"],[12,"# C","Bln"]],"cl":[{"p":[[null,"C","mille"],0,[3,"# C","mila"]],"x":[null,"C","mille"]},[3,"# C","mila"],[3,"# C","mila"],[[6,"# C","milione"],0,[6,"# C","milioni"]],[6,"# C","milioni"],[6,"# C","milioni"],[[9,"# C","miliardo"],0,[9,"# C","miliardi"]],[9,"# C","miliardi"],[9,"# C","miliardi"],[[12,"# C","mille miliardi"],0,[12,"# C","mila miliardi"]],[12,"# C","mila miliardi"],[12,"# C","mila miliardi"]],"cur":{"USD":[["dollaro statunitense","dollari statunitensi","dollari statunitensi"],"USD"],"EUR":["euro"],"GBP":[["sterlina britannica","sterline britanniche","sterline britanniche"]],"JPY":[["yen giapponese","yen giapponesi","yen giapponesi"],"JPY"],"CNY":[["yuan cinese","yuan cinesi","yuan cinesi"]],"INR":[["rupia indiana","rupie indiane","rupie indiane"],"INR"],"CAD":[["dollaro canadese","dollari canadesi","dollari canadesi"]],"AUD":[["dollaro australiano","dollari australiani","dollari australiani"]],"CHF":[["franco svizzero","franchi svizzeri","franchi svizzeri"]],"SEK":[["corona svedese","corone svedesi","corone svedesi"]],"NOK":[0,0,"NKr"],"PLN":[["zloty polacco","zloty polacchi","zloty polacchi"]],"RUB":[["rublo russo","rubli russi","rubli russi"]],"BRL":[["real brasiliano","real brasiliani","real brasiliani"],"BRL"],"MXN":[["peso messicano","pesos messicani","pesos messicani"],"MXN"],"KRW":[["won sudcoreano","won sudcoreani","won sudcoreani"],"KRW"],"HKD":[0,"HKD"],"TWD":[0,"TWD","NT$"],"THB":[0,"฿"],"VND":[0,"VND"],"EGP":[0,0,"£E"]},"unit":{"byte":{"l":"{0} byte","s":"{0} byte","n":"{0}B"},"kilobyte":{"l":"{0} kilobyte","s":"{0} kB","n":"{0}kB"},"megabyte":{"l":"{0} megabyte","s":"{0} MB","n":"{0}MB"},"gigabyte":{"l":"{0} gigabyte","s":"{0} GB","n":"{0}GB"},"terabyte":{"l":"{0} terabyte","s":"{0} TB","n":"{0}TB"},"percent":{"l":"{0} percento","s":"{0}%","n":0},"millisecond":{"l":["{0} millisecondo","{0} millisecondi","{0} millisecondi"],"s":"{0} ms","n":"{0}ms"},"second":{"l":["{0} secondo","{0} secondi","{0} secondi"],"s":"{0} s","n":"{0}s"},"minute":{"l":["{0} minuto","{0} minuti","{0} minuti"],"s":"{0} min","n":"{0}min"},"hour":{"l":["{0} ora","{0} ore","{0} ore"],"s":"{0} h","n":"{0}h"},"day":{"l":["{0} giorno","{0} giorni","{0} giorni"],"s":["{0} giorno","{0} giorni","{0} giorni"],"n":["{0}g","{0}gg","{0}gg"]},"week":{"l":["{0} settimana","{0} settimane","{0} settimane"],"s":"{0} sett.","n":"{0}sett."},"month":{"l":["{0} mese","{0} mesi","{0} mesi"],"s":["{0} mese","{0} mesi","{0} mesi"],"n":0},"year":{"l":["{0} anno","{0} anni","{0} anni"],"s":["{0} anno","{0} anni","{0} anni"],"n":["{0}anno","{0}anni","{0}anni"]},"kilometer":{"l":["{0} chilometro","{0} chilometri","{0} chilometri"],"s":"{0} km","n":"{0}km"},"kilometer-per-hour":{"l":["{0} chilometro orario","{0} chilometri orari","{0} chilometri orari"],"s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}-{1}","approx":"~","rngS":""},"rel":{"l":{"year":[["tra {0} anno","tra {0} anni","tra {0} anni"],["{0} anno fa","{0} anni fa","{0} anni fa"],{"0":"quest’anno","1":"anno prossimo","-1":"anno scorso"}],"quarter":[["tra {0} trimestre","tra {0} trimestri","tra {0} trimestri"],["{0} trimestre fa","{0} trimestri fa","{0} trimestri fa"],{"0":"questo trimestre","1":"trimestre prossimo","-1":"trimestre scorso"}],"month":[["tra {0} mese","tra {0} mesi","tra {0} mesi"],["{0} mese fa","{0} mesi fa","{0} mesi fa"],{"0":"questo mese","1":"mese prossimo","-1":"mese scorso"}],"week":[["tra {0} settimana","tra {0} settimane","tra {0} settimane"],["{0} settimana fa","{0} settimane fa","{0} settimane fa"],{"0":"questa settimana","1":"settimana prossima","-1":"settimana scorsa"}],"day":[["tra {0} giorno","tra {0} giorni","tra {0} giorni"],["{0} giorno fa","{0} giorni fa","{0} giorni fa"],{"0":"oggi","1":"domani","2":"dopodomani","-2":"l’altro ieri","-1":"ieri"}],"hour":[["tra {0} ora","tra {0} ore","tra {0} ore"],["{0} ora fa","{0} ore fa","{0} ore fa"],{"0":"quest’ora"}],"minute":[["tra {0} minuto","tra {0} minuti","tra {0} minuti"],["{0} minuto fa","{0} minuti fa","{0} minuti fa"],{"0":"questo minuto"}],"second":[["tra {0} secondo","tra {0} secondi","tra {0} secondi"],["{0} secondo fa","{0} secondi fa","{0} secondi fa"],{"0":"ora"}]},"s":{"year":[["tra {0} anno","tra {0} anni","tra {0} anni"],["{0} anno fa","{0} anni fa","{0} anni fa"],{"0":"quest’anno","1":"anno prossimo","-1":"anno scorso"}],"quarter":["tra {0} trim.","{0} trim. fa",{"0":"questo trim.","1":"trim. prossimo","-1":"trim. scorso"}],"month":[["tra {0} mese","tra {0} mesi","tra {0} mesi"],["{0} mese fa","{0} mesi fa","{0} mesi fa"],{"0":"questo mese","1":"mese prossimo","-1":"mese scorso"}],"week":["tra {0} sett.","{0} sett. fa",{"0":"questa sett.","1":"sett. prossima","-1":"sett. scorsa"}],"day":[["tra {0} g","tra {0} gg","tra {0} gg"],["{0} g fa","{0} gg fa","{0} gg fa"],{"0":"oggi","1":"domani","2":"dopodomani","-2":"l’altro ieri","-1":"ieri"}],"hour":["tra {0} h","{0} h fa",{"0":"quest’ora"}],"minute":["tra {0} min","{0} min fa",{"0":"questo minuto"}],"second":["tra {0} sec.","{0} sec. fa",{"0":"ora"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":["tra {0} s","{0} s fa",{"0":"ora"}]}},"list":{"cl":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"cs":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"cn":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"dl":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"ds":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"dn":["{0} o {1}","{0}, {1}","{0}, {1}","{0} o {1}"],"ul":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"us":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"fld":{"l":["era","anno","trimestre","mese","settimana","giorno della settimana","giorno","AM/PM","ora","minuto","secondo","fuso orario"],"s":["era","anno","trim.","mese","sett.","giorno settimana","g.","AM/PM","h","min","s","fuso"],"n":["era","anno","trim.","mese","sett.","giorno sett.","g.","AM/PM","h","min","s","fuso"]},"lp":["{0} ({1})",", "]}}`,
    "pt": `{"n":{"Ml":["janeiro","fevereiro","março","abril","maio","junho","julho","agosto","setembro","outubro","novembro","dezembro"],"Ll":0,"El":["domingo","segunda-feira","terça-feira","quarta-feira","quinta-feira","sexta-feira","sábado"],"cl":0,"Gl":["antes de Cristo","depois de Cristo"],"Bl":[0,"da madrugada",6,"da manhã",12,"meio-dia",13,"da tarde",19,"da noite"],"Bl0":["da madrugada","meio-dia"],"Ms":["jan.","fev.","mar.","abr.","mai.","jun.","jul.","ago.","set.","out.","nov.","dez."],"Ls":0,"Es":["dom.","seg.","ter.","qua.","qui.","sex.","sáb."],"cs":0,"Gs":["a.C.","d.C."],"Bs":[0,"da madrugada",6,"da manhã",12,"meio-dia",13,"da tarde",19,"da noite"],"Bs0":["da madrugada","meio-dia"],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["D","S","T","Q","Q","S","S"],"cn":0,"Gn":["a.C.","d.C."],"Bn":[0,"da madrugada",6,"da manhã",12,"meio-dia",13,"da tarde",19,"da noite"],"Bn0":["da madrugada","meio-dia"],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d2}/{M2}","{d}/{M}","{d} de {M}","{y}","{y} (dia: {d})","{M2}/{y}","{d2}/{M2}/{y}","{d}/{M}/{y}","{M}/{y}","{M} de {y}","{d} de {M} de {y}","{y} {d}","{d2}/{M2}/{y1} {G}","{G} (dia: {d})","{G} (mês: {M})","{G} {d2}/{M2}","{G} {d}/{M}","{G} {d} de {M}","{y} {G}","{y} {G} (dia: {d})","{M2}/{y} {G}","{d2}/{M2}/{y} {G}","{d}/{M2}/{y} {G}","{M}/{y} {G}","{d2}/{M}/{y} {G}","{d}/{M}/{y} {G}","{M} de {y} {G}","{d} de {M} de {y} {G}","{E}","{E}, {d}","{M} {E}","{E}, {d2}/{M2}","{E}, {d}/{M2}","{E}, {d2}/{M}","{E}, {d}/{M}","{E}, {d} de {M}","{E} (mês: {M})","{y} {E}","{y} {E}, {d}","{M2}/{y} {E}","{E}, {d2}/{M2}/{y}","{E}, {d}/{M2}/{y}","{M}/{y} {E}","{E}, {d2}/{M}/{y}","{E}, {d}/{M}/{y}","{M} de {y} {E}","{E}, {d} de {M} de {y}","{G} {E}","{G} {E}, {d}","{G} {E}, {d2}/{M2}","{G} {E}, {d}/{M2}","{G} {E}, {d2}/{M}","{G} {E}, {d}/{M}","{G} {E}, {d} de {M}","{y} {G} {E}","{y} {G} (dia: {E}, {d})","{M2}/{y} {G} {E}","{E}, {M2}/{d2}/{y} {G}","{E}, {M2}/{d}/{y} {G}","{M}/{y} {G} {E}","{E}, {M}/{d2}/{y} {G}","{E}, {M}/{d}/{y} {G}","{M} de {y} {G} {E}","{E}, {d} de {M} de {y} {G}","{y} {G} {E}, {d}"],"01123424425525525567789:;::<==<==<==6>789:;::<==<==<==?@@ABCACCADDADDADDEFFGHIJKLMNNMNNMNNEFFGHIJKLMNNMNNMNNOPPQRSQTUQVVQVVWVVXYYZ[]^_abccbccbccXYYZ[]^_abccbccbccdeeAfgAhiAjjAjjdjjkllmnopqrsttsttsttkllmnopqrsttsttsttOPPQRSWTUWVVQVVWVVXYYZ[]^_abccbccbccXYYZ[]^_abccbccbccdeeAfgdhidjjAjjdjjkllmnopqrsttsttsttkulmnopqrsttsttsttOPPQRSQTUQVVQVVQVVXYYZ[]^_abccbccbccXYYZ[]^_abccbccbccdeeAfgAhiAjjAjjAjjkllmnopqrsttsttsttkllmnopqrsttsttstt"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a}, {z}","{h} {a} (segundo: {s})","{h} {a}, {z} (segundo: {s})","{h} {a} (segundo: {s1})","{h} {a}, {z} (segundo: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (segundo: {s})","{h2} {z} (segundo: {s})","{h2} (segundo: {s1})","{h2} {z} (segundo: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h}","{h} {z}","{s} (hora: {h})","{h} {z} (segundo: {s})","{h} (segundo: {s1})","{h} {z} (segundo: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}"],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"30":"{E}, {h2}:{m}","34":"{E}, {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} às {0}","l":"{1} às {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {d1} de {Ml} de {y1}","{d1} de {Ml} de {y1}","{d1} de {Ms} de {y1}","{d2}/{M2}/{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} às {0}","{1} às {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","Horário Universal Coordenado"],"mz":{"5":["","","Horário Padrão da Europa Oriental","Horário de Verão da Europa Oriental","Horário da Europa Oriental"],"6":["","","Horário Padrão da Europa Central","Horário de Verão da Europa Central","Horário da Europa Central"],"12":["BRT","","Horário Padrão de Brasília","","Horário Padrão de Brasília"],"18":["","","Horário Padrão das Montanhas","Horário de Verão das Montanhas","Horário das Montanhas"],"22":["","","Horário Padrão Central","Horário de Verão Central","Horário Central"],"25":["","","Horário Padrão do Leste","Horário de Verão do Leste","Horário do Leste"],"35":["","","Horário Padrão do Pacífico","Horário de Verão do Pacífico","Horário do Pacífico"],"45":["","","Horário Padrão da Austrália Oriental","Horário de Verão da Austrália Oriental","Horário da Austrália Oriental"],"61":["","","Horário Padrão da Índia","","Horário Padrão da Índia"],"77":["","","Horário Padrão da China","","Horário Padrão da China"],"82":["","","Horário Padrão da Coreia","","Horário Padrão da Coreia"],"90":["","","Horário Padrão do Japão","","Horário Padrão do Japão"],"111":["","","Horário Padrão de Moscou","","Horário Padrão de Moscou"],"112":["","","Horário do Meridiano de Greenwich","Horário de Verão Britânico","Horário Reino Unido"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":".","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤ #","-¤ #"],"ca":["¤ #","-¤ #"],"cc":["¤ #","-¤ #"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","mil"],[3,"# C","mil"],[3,"# C","mil"],[6,"# C","mi"],[6,"# C","mi"],[6,"# C","mi"],[9,"# C","bi"],[9,"# C","bi"],[9,"# C","bi"],[12,"# C","tri"],[12,"# C","tri"],[12,"# C","tri"]],"cl":[[3,"# C","mil"],[3,"# C","mil"],[3,"# C","mil"],[[6,"# C","milhão"],0,[6,"# C","milhões"]],[6,"# C","milhões"],[6,"# C","milhões"],[[9,"# C","bilhão"],0,[9,"# C","bilhões"]],[9,"# C","bilhões"],[9,"# C","bilhões"],[[12,"# C","trilhão"],0,[12,"# C","trilhões"]],[12,"# C","trilhões"],[12,"# C","trilhões"]],"cur":{"USD":[["Dólar americano","Dólares americanos","Dólares americanos"],"US$"],"EUR":[["Euro","Euros","Euros"]],"GBP":[["Libra esterlina","Libras esterlinas","Libras esterlinas"]],"JPY":[["Iene japonês","Ienes japoneses","Ienes japoneses"],"JP¥"],"CNY":[["Yuan chinês","Yuans chineses","Yuans chineses"]],"INR":[["Rupia indiana","Rupias indianas","Rupias indianas"]],"CAD":[["Dólar canadense","Dólares canadenses","Dólares canadenses"]],"AUD":[["Dólar australiano","Dólares australianos","Dólares australianos"],"AU$"],"CHF":[["Franco suíço","Francos suíços","Francos suíços"]],"SEK":[["Coroa sueca","Coroas suecas","Coroas suecas"]],"PLN":[["Zloty polonês","Zlotys poloneses","Zlotys poloneses"]],"RUB":[["Rublo russo","Rublos russos","Rublos russos"]],"BRL":[["Real brasileiro","Reais brasileiros","Reais brasileiros"]],"MXN":[["Peso mexicano","Pesos mexicanos","Pesos mexicanos"]],"KRW":[["Won sul-coreano","Wons sul-coreanos","Wons sul-coreanos"]],"TWD":[0,0,"NT$"],"THB":[0,"฿"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":["{0} byte","{0} bytes","{0} bytes"],"s":"{0} bytes","n":"{0} B"},"kilobyte":{"l":["{0} kilobyte","{0} kilobytes","{0} kilobytes"],"s":"{0} kB","n":0},"megabyte":{"l":["{0} megabyte","{0} megabytes","{0} megabytes"],"s":"{0} MB","n":0},"gigabyte":{"l":["{0} gigabyte","{0} gigabytes","{0} gigabytes"],"s":"{0} GB","n":0},"terabyte":{"l":["{0} terabyte","{0} terabytes","{0} terabytes"],"s":"{0} TB","n":0},"percent":{"l":"{0} por cento","s":"{0}%","n":0},"millisecond":{"l":["{0} milissegundo","{0} milissegundos","{0} milissegundos"],"s":"{0} ms","n":0},"second":{"l":["{0} segundo","{0} segundos","{0} segundos"],"s":"{0} s","n":0},"minute":{"l":["{0} minuto","{0} minutos","{0} minutos"],"s":"{0} min","n":0},"hour":{"l":["{0} hora","{0} horas","{0} horas"],"s":"{0} h","n":0},"day":{"l":["{0} dia","{0} dias","{0} dias"],"s":["{0} dia","{0} dias","{0} dias"],"n":0},"week":{"l":["{0} semana","{0} semanas","{0} semanas"],"s":"{0} sem.","n":0},"month":{"l":["{0} mês","{0} meses","{0} meses"],"s":["{0} mês","{0} meses","{0} meses"],"n":0},"year":{"l":["{0} ano","{0} anos","{0} anos"],"s":["{0} ano","{0} anos","{0} anos"],"n":0},"kilometer":{"l":["{0} quilômetro","{0} quilômetros","{0} quilômetros"],"s":"{0} km","n":0},"kilometer-per-hour":{"l":["{0} quilômetro por hora","{0} quilômetros por hora","{0} quilômetros por hora"],"s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}–{1}","approx":"~","rngS":""},"rel":{"l":{"year":[["em {0} ano","em {0} anos","em {0} anos"],["há {0} ano","há {0} anos","há {0} anos"],{"0":"este ano","1":"próximo ano","-1":"ano passado"}],"quarter":[["em {0} trimestre","em {0} trimestres","em {0} trimestres"],["há {0} trimestre","há {0} trimestres","há {0} trimestres"],{"0":"este trimestre","1":"próximo trimestre","-1":"último trimestre"}],"month":[["em {0} mês","em {0} meses","em {0} meses"],["há {0} mês","há {0} meses","há {0} meses"],{"0":"este mês","1":"próximo mês","-1":"mês passado"}],"week":[["em {0} semana","em {0} semanas","em {0} semanas"],["há {0} semana","há {0} semanas","há {0} semanas"],{"0":"esta semana","1":"próxima semana","-1":"semana passada"}],"day":[["em {0} dia","em {0} dias","em {0} dias"],["há {0} dia","há {0} dias","há {0} dias"],{"0":"hoje","1":"amanhã","2":"depois de amanhã","-2":"anteontem","-1":"ontem"}],"hour":[["em {0} hora","em {0} horas","em {0} horas"],["há {0} hora","há {0} horas","há {0} horas"],{"0":"esta hora"}],"minute":[["em {0} minuto","em {0} minutos","em {0} minutos"],["há {0} minuto","há {0} minutos","há {0} minutos"],{"0":"este minuto"}],"second":[["em {0} segundo","em {0} segundos","em {0} segundos"],["há {0} segundo","há {0} segundos","há {0} segundos"],{"0":"agora"}]},"s":{"year":[["em {0} ano","em {0} anos","em {0} anos"],["há {0} ano","há {0} anos","há {0} anos"],{"0":"este ano","1":"próximo ano","-1":"ano passado"}],"quarter":["em {0} trim.","há {0} trim.",{"0":"este trimestre","1":"próximo trimestre","-1":"último trimestre"}],"month":[["em {0} mês","em {0} meses","em {0} meses"],["há {0} mês","há {0} meses","há {0} meses"],{"0":"este mês","1":"próximo mês","-1":"mês passado"}],"week":["em {0} sem.","há {0} sem.",{"0":"esta semana","1":"próxima semana","-1":"semana passada"}],"day":[["em {0} dia","em {0} dias","em {0} dias"],["há {0} dia","há {0} dias","há {0} dias"],{"0":"hoje","1":"amanhã","2":"depois de amanhã","-2":"anteontem","-1":"ontem"}],"hour":["em {0} h","há {0} h",{"0":"esta hora"}],"minute":["em {0} min.","há {0} min.",{"0":"este minuto"}],"second":["em {0} seg.","há {0} seg.",{"0":"agora"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"cs":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"ds":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"dn":["{0} ou {1}","{0}, {1}","{0}, {1}","{0} ou {1}"],"ul":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"us":["{0} e {1}","{0}, {1}","{0}, {1}","{0} e {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"fld":{"l":["era","ano","trimestre","mês","semana","dia da semana","dia","AM/PM","hora","minuto","segundo","fuso horário"],"s":["era","ano","trim.","mês","sem.","dia da sem.","dia","AM/PM","h","min.","seg.","fuso"],"n":["era","ano","trim.","mês","sem.","dia da sem.","dia","AM/PM","h","min","seg.","fuso"]},"lp":["{0} ({1})",", "]}}`,
    "nl": `{"n":{"Ml":["januari","februari","maart","april","mei","juni","juli","augustus","september","oktober","november","december"],"Ll":0,"El":["zondag","maandag","dinsdag","woensdag","donderdag","vrijdag","zaterdag"],"cl":0,"Gl":["voor Christus","na Christus"],"Bl":[0,"’s nachts",6,"’s ochtends",12,"’s middags",18,"’s avonds"],"Bl0":["’s nachts","’s middags"],"Ms":["jan","feb","mrt","apr","mei","jun","jul","aug","sep","okt","nov","dec"],"Ls":0,"Es":["zo","ma","di","wo","do","vr","za"],"cs":0,"Gs":["v.Chr.","n.Chr."],"Bs":[0,"’s nachts",6,"’s ochtends",12,"’s middags",18,"’s avonds"],"Bs0":["’s nachts","’s middags"],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["Z","M","D","W","D","V","Z"],"cn":0,"Gn":["v.C.","n.C."],"Bn":[0,"’s nachts",6,"’s ochtends",12,"’s middags",18,"’s avonds"],"Bn0":["’s nachts","’s middags"],"a":["a.m.","p.m."]},"dp":[["","{d}","{M}","{d}-{M}","{d} {M}","{y}","{y} (dag: {d})","{M}-{y}","{d}-{M}-{y}","{M} {y}","{d} {M} {y}","{y} {d}","{d1}/{M1}/{y1} {G}","{G} (dag: {d})","{G} (maand: {M})","{G} {d}-{M}","{G} {d} {M}","{y} {G}","{y} {G} (dag: {d})","{M}/{y} {G}","{d}/{M}/{y} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E} {d}-{M}","{E} {d} {M}","{E} (maand: {M})","{y} {E}","{y} {E} {d}","{M}-{y} {E}","{E} {d}-{M}-{y}","{M} {y} {E}","{E} {d} {M} {y}","{G} {E}","{G} {E} {d}","{G} {E} {d}-{M}","{G} {E} {d} {M}","{y} {G} {E}","{y} {G} (dag: {E} {d})","{M}/{y} {G} {E}","{E} {d}/{M}/{y} {G}","{M} {y} {G} {E}","{E} {d} {M} {y} {G}","{y} {G} {E} {d}"],"0112332332442442445667887889::9::9::5;67887889::9::9::<==>??>??>@@>@@>@@ABBCDDCDDEFFEFFEFFABBCDDCDDEFFEFFEFFGHHIJJIJJIKKIKKLKKMNNOPPOPPQRRQRRQRRMNNOPPOPPQRRQRRQRRSTT>UU>UU>VV>VVSVVWXXYZZYZZ[]][]][]]WXXYZZYZZ[]][]][]]GHHIJJLJJLKKIKKLKKMNNOPPOPPQRRQRRQRRMNNOPPOPPQRRQRRQRRSTT>UUSUUSVV>VVSVVWXXYZZYZZ[]][]][]]W^XYZZYZZ[]][]][]]GHHIJJIJJIKKIKKIKKMNNOPPOPPQRRQRRQRRMNNOPPOPPQRRQRRQRRSTT>UU>UU>VV>VV>VVWXXYZZYZZ[]][]][]]WXXYZZYZZ[]][]][]]"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (seconde: {s})","{h} {a} {z} (seconde: {s})","{h} {a} (seconde: {s1})","{h} {a} {z} (seconde: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (seconde: {s})","{h2} {z} (seconde: {s})","{h2} (seconde: {s1})","{h2} {z} (seconde: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h}","{h} {z}","{s} (uur: {h})","{h} {z} (seconde: {s})","{h} (seconde: {s1})","{h} {z} (seconde: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} om {0}","l":"{1} om {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El} {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d2}-{M2}-{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} om {0}","{1} om {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","gecoördineerde wereldtijd"],"mz":{"5":["EET","EEST","Oost-Europese standaardtijd","Oost-Europese zomertijd","Oost-Europese tijd"],"6":["CET","CEST","Midden-Europese standaardtijd","Midden-Europese zomertijd","Midden-Europese tijd"],"12":["","","Braziliaanse standaardtijd","","Braziliaanse standaardtijd"],"18":["MST","MDT","Mountain-standaardtijd","Mountain-zomertijd","Mountain-tijd"],"22":["CST","CDT","Central-standaardtijd","Central-zomertijd","Central-tijd"],"25":["EST","EDT","Eastern-standaardtijd","Eastern-zomertijd","Eastern-tijd"],"35":["PST","PDT","Pacific-standaardtijd","Pacific-zomertijd","Pacific-tijd"],"45":["","","Oost-Australische standaardtijd","Oost-Australische zomertijd","Oost-Australische tijd"],"61":["","","Indiase tijd","","Indiase tijd"],"77":["","","Chinese standaardtijd","","Chinese standaardtijd"],"82":["","","Koreaanse standaardtijd","","Koreaanse standaardtijd"],"90":["","","Japanse standaardtijd","","Japanse standaardtijd"],"111":["","","Moskou-standaardtijd","","Moskou-standaardtijd"],"112":["GMT","","Greenwich Mean Time","Britse zomertijd","tijd in Verenigd Koninkrijk"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":".","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤ #","¤ -#"],"ca":["¤ #","(¤ #)"],"cc":["¤ #","¤ -#"],"cn":["# ¤","-# ¤"]},"cs":[[3,"#C","K"],[3,"#C","K"],[3,"#C","K"],[6,"# C","mln."],[6,"# C","mln."],[6,"# C","mln."],[9,"# C","mld."],[9,"# C","mld."],[9,"# C","mld."],[12,"# C","bln."],[12,"# C","bln."],[12,"# C","bln."]],"cl":[[3,"# C","duizend"],[3,"# C","duizend"],[3,"# C","duizend"],[6,"# C","miljoen"],[6,"# C","miljoen"],[6,"# C","miljoen"],[9,"# C","miljard"],[9,"# C","miljard"],[9,"# C","miljard"],[12,"# C","biljoen"],[12,"# C","biljoen"],[12,"# C","biljoen"]],"cur":{"USD":["Amerikaanse dollar","US$"],"EUR":["euro"],"GBP":["Britse pond"],"JPY":["Japanse yen","JP¥"],"CNY":["Chinese yuan"],"INR":["Indiase roepie"],"CAD":["Canadese dollar","C$"],"AUD":["Australische dollar","AU$"],"CHF":["Zwitserse frank"],"SEK":[["Zweedse kroon","Zweedse kronen"]],"PLN":["Poolse zloty"],"RUB":["Russische roebel"],"BRL":["Braziliaanse real"],"MXN":["Mexicaanse peso"],"KRW":["Zuid-Koreaanse won"],"TWD":[0,0,"NT$"],"THB":[0,"฿"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":"{0} byte","s":"{0} byte","n":0},"kilobyte":{"l":"{0} kilobyte","s":"{0} kB","n":0},"megabyte":{"l":"{0} megabyte","s":"{0} MB","n":0},"gigabyte":{"l":"{0} gigabyte","s":"{0} GB","n":0},"terabyte":{"l":"{0} terabyte","s":"{0} TB","n":0},"percent":{"l":"{0} procent","s":"{0}%","n":0},"millisecond":{"l":["{0} milliseconde","{0} milliseconden"],"s":"{0} ms","n":0},"second":{"l":["{0} seconde","{0} seconden"],"s":"{0} sec","n":"{0} s"},"minute":{"l":["{0} minuut","{0} minuten"],"s":"{0} min","n":"{0} m"},"hour":{"l":"{0} uur","s":"{0} uur","n":"{0} u"},"day":{"l":["{0} dag","{0} dagen"],"s":["{0} dag","{0} dagen"],"n":"{0} d"},"week":{"l":["{0} week","{0} weken"],"s":["{0} wk","{0} wkn"],"n":"{0} w"},"month":{"l":["{0} maand","{0} maanden"],"s":"{0} mnd","n":"{0} m"},"year":{"l":"{0} jaar","s":"{0} jr","n":0},"kilometer":{"l":"{0} kilometer","s":"{0} km","n":0},"kilometer-per-hour":{"l":"{0} kilometer per uur","s":"{0} km/u","n":0}},"rng":"{0}-{1}","approx":"~","rngS":""},"rel":{"l":{"year":["over {0} jaar","{0} jaar geleden",{"0":"dit jaar","1":"volgend jaar","-1":"vorig jaar"}],"quarter":[["over {0} kwartaal","over {0} kwartalen"],["{0} kwartaal geleden","{0} kwartalen geleden"],{"0":"dit kwartaal","1":"volgend kwartaal","-1":"vorig kwartaal"}],"month":[["over {0} maand","over {0} maanden"],["{0} maand geleden","{0} maanden geleden"],{"0":"deze maand","1":"volgende maand","-1":"vorige maand"}],"week":[["over {0} week","over {0} weken"],["{0} week geleden","{0} weken geleden"],{"0":"deze week","1":"volgende week","-1":"vorige week"}],"day":[["over {0} dag","over {0} dagen"],["{0} dag geleden","{0} dagen geleden"],{"0":"vandaag","1":"morgen","2":"overmorgen","-2":"eergisteren","-1":"gisteren"}],"hour":["over {0} uur","{0} uur geleden",{"0":"binnen een uur"}],"minute":[["over {0} minuut","over {0} minuten"],["{0} minuut geleden","{0} minuten geleden"],{"0":"binnen een minuut"}],"second":[["over {0} seconde","over {0} seconden"],["{0} seconde geleden","{0} seconden geleden"],{"0":"nu"}]},"s":{"year":["over {0} jaar","{0} jaar geleden",{"0":"dit jaar","1":"volgend jaar","-1":"vorig jaar"}],"quarter":["over {0} kwart.","{0} kwart. geleden",{"0":"dit kwartaal","1":"volgend kwartaal","-1":"vorig kwartaal"}],"month":[["over {0} maand","over {0} maanden"],["{0} maand geleden","{0} maanden geleden"],{"0":"deze maand","1":"volgende maand","-1":"vorige maand"}],"week":[["over {0} week","over {0} weken"],["{0} week geleden","{0} weken geleden"],{"0":"deze week","1":"volgende week","-1":"vorige week"}],"day":[["over {0} dag","over {0} dgn"],["{0} dag geleden","{0} dgn geleden"],{"0":"vandaag","1":"morgen","2":"overmorgen","-2":"eergisteren","-1":"gisteren"}],"hour":["over {0} uur","{0} uur geleden",{"0":"binnen een uur"}],"minute":["over {0} min.","{0} min. geleden",{"0":"binnen een minuut"}],"second":["over {0} sec.","{0} sec. geleden",{"0":"nu"}]},"n":{"year":0,"quarter":["over {0} kw.","{0} kw. geleden",{"0":"dit kwartaal","1":"volgend kwartaal","-1":"vorig kwartaal"}],"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} en {1}","{0}, {1}","{0}, {1}","{0} en {1}"],"cs":["{0} & {1}","{0}, {1}","{0}, {1}","{0} & {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} of {1}","{0}, {1}","{0}, {1}","{0} of {1}"],"ds":["{0} of {1}","{0}, {1}","{0}, {1}","{0} of {1}"],"dn":["{0} of {1}","{0}, {1}","{0}, {1}","{0} of {1}"],"ul":["{0} en {1}","{0}, {1}","{0}, {1}","{0} en {1}"],"us":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"un":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"]},"dn":{"fld":{"l":["tijdperk","jaar","kwartaal","maand","week","dag van de week","dag","a.m./p.m.","uur","minuut","seconde","tijdzone"],"s":["tijdperk","jr","kwartaal","mnd","wk","dag van de wk","dag","a.m./p.m.","uur","min","sec","zone"],"n":["tijdperk","jr","kwartaal","mnd","wk","dag v.d. wk","dag","a.m./p.m.","u","min","s","zone"]},"lp":["{0} ({1})",", "]}}`,
    "sv": `{"n":{"Ml":["januari","februari","mars","april","maj","juni","juli","augusti","september","oktober","november","december"],"Ll":0,"El":["söndag","måndag","tisdag","onsdag","torsdag","fredag","lördag"],"cl":0,"Gl":["före Kristus","efter Kristus"],"Bl":[0,"på natten",5,"på morgonen",10,"på förmiddagen",12,"på eftermiddagen",18,"på kvällen"],"Bl0":["på natten","på eftermiddagen"],"Ms":["jan.","feb.","mars","apr.","maj","juni","juli","aug.","sep.","okt.","nov.","dec."],"Ls":0,"Es":["sön","mån","tis","ons","tors","fre","lör"],"cs":0,"Gs":["f.Kr.","e.Kr."],"Bs":[0,"på natten",5,"på morg.",10,"på förm.",12,"på efterm.",18,"på kvällen"],"Bs0":["på natten","på efterm."],"Mn":["J","F","M","A","M","J","J","A","S","O","N","D"],"Ln":0,"En":["S","M","T","O","T","F","L"],"cn":0,"Gn":["f.Kr.","e.Kr."],"Bn":[0,"på natten",5,"på morg.",10,"på förm.",12,"på efterm.",18,"på kvällen"],"Bn0":["på natten","på efterm."],"a":["fm","em"]},"dp":[["","{d}","{M}","{d}/{M}","{d}/{M1}","{d} {M}","{y}","{y} (dag: {d})","{y}-{M2}","{y}-{M2}-{d2}","{y}-{M}-{d}","{y}-{M}","{M} {y}","{d} {M} {y}","{y} {d}","{y1}-{M2}-{d2} {G}","{G} (dag: {d})","{G} (månad: {M})","{G} {d}/{M}","{G} {d}/{M1}","{G} {d} {M}","{y} {G}","{y} {G} (dag: {d})","{y}-{M2} {G}","{y}-{M2}-{d2} {G}","{y}-{M2}-{d} {G}","{y}-{M} {G}","{y}-{M}-{d2} {G}","{y}-{M}-{d} {G}","{M} {y} {G}","{d} {M} {y} {G}","{E}","{E} {d}","{M} {E}","{E} {d}/{M}","{E} {d} {M}","{E} (månad: {M})","{y} {E}","{y} {E} {d}","{y}-{M2} {E}","{E}, {y}-{M2}-{d2}","{E}, {y}-{M2}-{d}","{y}-{M} {E}","{E}, {y}-{M}-{d2}","{E}, {y}-{M}-{d}","{M} {y} {E}","{E} {d} {M} {y}","{G} {E}","{G} {E} {d}","{G} {E} {d}/{M}","{G} {E} {d} {M}","{y} {G} {E}","{y} {G} (dag: {E} {d})","{y}-{M2} {G} {E}","{E} {M}-{d}-{y} {G}","{y}-{M} {G} {E}","{M} {y} {G} {E}","{E} {d} {M} {y} {G}","{y} {G} {E} {d}"],"01123324325525525567789:;::<==<==<==6>789:;::<==<==<==?@@ABBACBADDADDADDEFFGHIJKLMNNMNNMNNEFFGHIJKLMNNMNNMNNOPPQRRQRRQSSQSSTSSUVVWXYZ[]^__^__^__UVVWXYZ[]^__^__^__abbAccAccAddAddaddeffghhihhjkkjkkjkkeffghhihhjkkjkkjkkOPPQRRTRRTSSQSSTSSUVVWXYZ[]^__^__^__UVVWXYZ[]^__^__^__abbAccaccaddAddaddeffghhihhjkkjkkjkkelfghhihhjkkjkkjkkOPPQRRQRRQSSQSSQSSUVVWXYZ[]^__^__^__UVVWXYZ[]^__^__^__abbAccAccAddAddAddeffghhihhjkkjkkjkkeffghhihhjkkjkkjkk"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (sekund: {s})","{h} {a} {z} (sekund: {s})","{h} {a} (sekund: {s1})","{h} {a} {z} (sekund: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (sekund: {s})","{h2} {z} (sekund: {s})","{h2} (sekund: {s1})","{h2} {z} (sekund: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h}","{h} {z}","{s} (timme: {h})","{h} {z} (sekund: {s})","{h} (sekund: {s1})","{h} {z} (sekund: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1} {0}","{1} {0}","{1} {0}","{1} {0}"],["{1} {0}","{1} {0}","{1} {0}","{1} {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} kl. {0}","l":"{1} kl. {0}","m":"{1} {0}","s":"{1} {0}"},"st":{"d":["{El} {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{y1}-{M2}-{d2}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} kl. {0}","{1} kl. {0}","{1} {0}","{1} {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","−HH:mm","UTC","koordinerad universell tid"],"mz":{"5":["EET","EEST","östeuropeisk normaltid","östeuropeisk sommartid","östeuropeisk tid"],"6":["CET","CEST","centraleuropeisk normaltid","centraleuropeisk sommartid","centraleuropeisk tid"],"12":["","","Brasilia, normaltid","","Brasilia, normaltid"],"18":["","","Klippiga bergen, normaltid","Klippiga bergen, sommartid","Klippiga bergentid"],"22":["","","centralnordamerikansk normaltid","centralnordamerikansk sommartid","centralnordamerikansk tid"],"25":["","","östnordamerikansk normaltid","östnordamerikansk sommartid","östnordamerikansk tid"],"35":["","","västnordamerikansk normaltid","västnordamerikansk sommartid","västnordamerikansk tid"],"45":["","","östaustralisk normaltid","östaustralisk sommartid","östaustralisk tid"],"61":["","","indisk tid","","indisk tid"],"77":["","","kinesisk normaltid","","kinesisk normaltid"],"82":["","","koreansk normaltid","","koreansk normaltid"],"90":["","","japansk normaltid","","japansk normaltid"],"111":["","","Moskva, normaltid","","Moskva, normaltid"],"112":["GMT","","Greenwichtid","brittisk sommartid","Storbritannientid"]},"zg":{},"rs":"–","fs":",","num":{"dec":",","grp":" ","min":"−","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"×10^","pat":{"d":["#","-#"],"p":["# %","-# %"],"c":["# ¤","-# ¤"],"ca":["# ¤","-# ¤"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","tn"],[3,"# C","tn"],[3,"# C","tn"],[6,"# C","mn"],[6,"# C","mn"],[6,"# C","mn"],[9,"# C","md"],[9,"# C","md"],[9,"# C","md"],[12,"# C","bn"],[12,"# C","bn"],[12,"# C","bn"]],"cl":[[3,"# C","tusen"],[3,"# C","tusen"],[3,"# C","tusen"],[[6,"# C","miljon"],[6,"# C","miljoner"]],[6,"# C","miljoner"],[6,"# C","miljoner"],[[9,"# C","miljard"],[9,"# C","miljarder"]],[9,"# C","miljarder"],[9,"# C","miljarder"],[[12,"# C","biljon"],[12,"# C","biljoner"]],[12,"# C","biljoner"],[12,"# C","biljoner"]],"cur":{"USD":[["amerikansk dollar","amerikanska dollar"],"US$"],"EUR":["euro"],"GBP":[["brittiskt pund","brittiska pund"],"GBP"],"JPY":[["japansk yen","japanska yen"],"JPY"],"CNY":[["kinesisk yuan","kinesiska yuan"],"CNY"],"INR":[["indisk rupie","indiska rupier"],"INR"],"CAD":[["kanadensisk dollar","kanadensiska dollar"]],"AUD":[["australisk dollar","australiska dollar"],"AUD"],"NZD":[0,"NZD"],"CHF":[["schweizisk franc","schweiziska franc"]],"SEK":[["svensk krona","svenska kronor"],"kr"],"NOK":[0,"Nkr"],"DKK":[0,"Dkr"],"PLN":[["polsk zloty","polska zloty"]],"RUB":[["rysk rubel","ryska rubel"]],"BRL":[["brasiliansk real","brasilianska real"],"BR$"],"MXN":[["mexikansk peso","mexikanska pesos"]],"KRW":[["sydkoreansk won","sydkoreanska won"],"KRW"],"HKD":[0,"HKD"],"TWD":[0,"TWD","NT$"],"PHP":[0,"PHP"],"VND":[0,"VND"],"EGP":[0,"EG£"]},"unit":{"byte":{"l":"{0} byte","s":"{0} B","n":"{0}B"},"kilobyte":{"l":"{0} kilobyte","s":"{0} kB","n":"{0}kB"},"megabyte":{"l":"{0} megabyte","s":"{0} MB","n":"{0}MB"},"gigabyte":{"l":"{0} gigabyte","s":"{0} GB","n":"{0}GB"},"terabyte":{"l":"{0} terabyte","s":"{0} TB","n":"{0}TB"},"percent":{"l":"{0} procent","s":"{0} %","n":0},"millisecond":{"l":["{0} millisekund","{0} millisekunder"],"s":"{0} ms","n":"{0}ms"},"second":{"l":["{0} sekund","{0} sekunder"],"s":"{0} s","n":"{0}s"},"minute":{"l":["{0} minut","{0} minuter"],"s":"{0} min","n":"{0}m"},"hour":{"l":["{0} timme","{0} timmar"],"s":"{0} tim","n":"{0}h"},"day":{"l":"{0} dygn","s":"{0} d","n":"{0}d"},"week":{"l":["{0} vecka","{0} veckor"],"s":"{0} v","n":"{0}v"},"month":{"l":["{0} månad","{0} månader"],"s":"{0} mån","n":"{0}m"},"year":{"l":"{0} år","s":"{0} år","n":"{0}å"},"kilometer":{"l":"{0} kilometer","s":"{0} km","n":"{0}km"},"kilometer-per-hour":{"l":"{0} kilometer per timme","s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}–{1}","approx":"~","rngS":""},"rel":{"l":{"year":["om {0} år","för {0} år sedan",{"0":"i år","1":"nästa år","-1":"förra året"}],"quarter":["om {0} kvartal","för {0} kvartal sedan",{"0":"detta kvartal","1":"nästa kvartal","-1":"förra kvartalet"}],"month":[["om {0} månad","om {0} månader"],["för {0} månad sedan","för {0} månader sedan"],{"0":"den här månaden","1":"nästa månad","-1":"förra månaden"}],"week":[["om {0} vecka","om {0} veckor"],["för {0} vecka sedan","för {0} veckor sedan"],{"0":"denna vecka","1":"nästa vecka","-1":"förra veckan"}],"day":[["om {0} dag","om {0} dagar"],["för {0} dag sedan","för {0} dagar sedan"],{"0":"i dag","1":"i morgon","2":"i övermorgon","-2":"i förrgår","-1":"i går"}],"hour":[["om {0} timme","om {0} timmar"],["för {0} timme sedan","för {0} timmar sedan"],{"0":"denna timme"}],"minute":[["om {0} minut","om {0} minuter"],["för {0} minut sedan","för {0} minuter sedan"],{"0":"denna minut"}],"second":[["om {0} sekund","om {0} sekunder"],["för {0} sekund sedan","för {0} sekunder sedan"],{"0":"nu"}]},"s":{"year":["om {0} år","för {0} år sen",{"0":"i år","1":"nästa år","-1":"förra året"}],"quarter":["om {0} kv.","för {0} kv. sen",{"0":"detta kv.","1":"nästa kv.","-1":"förra kv."}],"month":["om {0} mån.","för {0} mån. sen",{"0":"denna mån.","1":"nästa mån.","-1":"förra mån."}],"week":["om {0} v.","för {0} v. sedan",{"0":"denna v.","1":"nästa v.","-1":"förra v."}],"day":["om {0} d",["för {0} d sedan","för {0} d sedan"],{"0":"i dag","1":"i morgon","2":"i övermorgon","-2":"i förrgår","-1":"i går"}],"hour":["om {0} tim","för {0} tim sedan",{"0":"denna timme"}],"minute":["om {0} min","för {0} min sen",{"0":"denna minut"}],"second":[["om {0} sek","om {0} sek"],"för {0} s sen",{"0":"nu"}]},"n":{"year":["+{0} år","−{0} år",{"0":"i år","1":"nästa år","-1":"förra året"}],"quarter":["+{0} kv.","−{0} kv",{"0":"detta kv.","1":"nästa kv.","-1":"förra kv."}],"month":["+{0} mån.","−{0} mån",{"0":"denna mån.","1":"nästa mån.","-1":"förra mån."}],"week":["+{0} v.","−{0} v",{"0":"denna v.","1":"nästa v.","-1":"förra v."}],"day":["+{0} d","−{0} d",{"0":"idag","1":"imorgon","2":"i övermorgon","-2":"i förrgår","-1":"igår"}],"hour":["+{0} h","−{0} h",{"0":"denna timme"}],"minute":["+{0} min","−{0} min",{"0":"denna minut"}],"second":["+{0} s","−{0} s",{"0":"nu"}]}},"list":{"cl":["{0} och {1}","{0}, {1}","{0}, {1}","{0} och {1}"],"cs":["{0} och {1}","{0}, {1}","{0}, {1}","{0} och {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} eller {1}","{0}, {1}","{0}, {1}","{0} eller {1}"],"ds":["{0} eller {1}","{0}, {1}","{0}, {1}","{0} eller {1}"],"dn":["{0} eller {1}","{0}, {1}","{0}, {1}","{0} eller {1}"],"ul":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"us":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"fld":{"l":["era","år","kvartal","månad","vecka","veckodag","dag","fm/em","timme","minut","sekund","tidszon"],"s":["era","år","kv.","mån","v.","veckod.","d.","fm/em","tim","min","sek","tidszon"],"n":["era","år","kv.","m","v","vd","d","fm/em","h","m","s","tidszon"]},"lp":["{0} ({1})",", "]}}`,
    "pl": `{"n":{"Ml":["stycznia","lutego","marca","kwietnia","maja","czerwca","lipca","sierpnia","września","października","listopada","grudnia"],"Ll":["styczeń","luty","marzec","kwiecień","maj","czerwiec","lipiec","sierpień","wrzesień","październik","listopad","grudzień"],"El":["niedziela","poniedziałek","wtorek","środa","czwartek","piątek","sobota"],"cl":0,"Gl":["przed naszą erą","naszej ery"],"Bl":[0,"w nocy",6,"rano",10,"przed południem",12,"w południe",13,"po południu",18,"wieczorem",21,"w nocy"],"Bl0":["w nocy","w południe"],"Ms":["sty","lut","mar","kwi","maj","cze","lip","sie","wrz","paź","lis","gru"],"Ls":0,"Es":["niedz.","pon.","wt.","śr.","czw.","pt.","sob."],"cs":0,"Gs":["p.n.e.","n.e."],"Bs":[0,"w nocy",6,"rano",10,"przed południem",12,"w południe",13,"po południu",18,"wieczorem",21,"w nocy"],"Bs0":["w nocy","w południe"],"Mn":["s","l","m","k","m","c","l","s","w","p","l","g"],"Ln":["S","L","M","K","M","C","L","S","W","P","L","G"],"En":["n","p","w","ś","c","p","s"],"cn":["N","P","W","Ś","C","P","S"],"Gn":["p.n.e.","n.e."],"Bn":[0,"w nocy",6,"rano",10,"przed poł.",12,"w poł.",13,"po poł.",18,"wiecz.",21,"w nocy"],"Bn0":["w nocy","w poł."],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d}.{M2}","{d}.{M}","{L}","{d} {M}","{y}","{y} (dzień: {d})","{M2}.{y}","{d}.{M2}.{y}","{M}.{y}","{d}.{M}.{y}","{L} {y}","{d} {M} {y}","{M} {y}","{y} {d}","{d1}.{M2}.{y1} {G}","{G} (dzień: {d})","{G} (miesiąc: {M})","{G} {d}.{M2}","{G} {d}.{M}","{G} (miesiąc: {L})","{G} {d} {M}","{y} {G}","{y} {G} (dzień: {d})","{M2}.{y} {G}","{d}.{M2}.{y} {G}","{M}.{y} {G}","{d}.{M}.{y} {G}","{L} {y} {G}","{d} {M} {y} {G}","{M} {y} {G}","{E}","{E}, {d}","{M} {E}","{E}, {d}.{M2}","{E}, {d}.{M}","{L} {E}","{E}, {d} {M}","{E} (miesiąc: {L})","{y} {E}","{y} {E}, {d}","{M2}.{y} {E}","{E}, {d}.{M2}.{y}","{M}.{y} {E}","{E}, {d}.{M}.{y}","{L} {y} {E}","{E}, {d} {M} {y}","{M} {y} {E}","{G} {E}","{G} {E}, {d}","{G} {E}, {d}.{M2}","{G} {E}, {d}.{M}","{G} {E}, {d} {M}","{y} {G} {E}","{y} {G} (dzień: {E}, {d})","{M2}.{y} {G} {E}","{E}, {d}.{M2}.{y} {G}","{M}.{y} {G} {E}","{E}, {d}.{M}.{y} {G}","{L} {y} {G} {E}","{E}, {d} {M} {y} {G}","{M} {y} {G} {E}","{E} (miesiąc: {M})","{y} {G} {E}, {d}","{c}","{M} {c}","{L} {c}","{y} {c}","{M2}.{y} {c}","{M}.{y} {c}","{L} {y} {c}","{M} {y} {c}","{G} {c}","{y} {G} {c}","{M2}.{y} {G} {c}","{M}.{y} {G} {c}","{L} {y} {G} {c}","{M} {y} {G} {c}"],"0112332445662665667889::;<<=>>?>>=>>7@89::;<<=>>?>>=>>ABBCDDCEEFGGCGGFGGHIIJKKLMMNOOPOOPOOHIIJKKLMMNOOPOOPOOQRRSTTSUUVWWSWWXWWYZZ[]]^__abbcbbabbYZZ[]]^__abbcbbabbdeeCffCggFhhChhdhhijjkllmnnoppqppqppijjkllmnnoppqppqppQRRSTTrUUXWWSWWXWWYZZ[]]^__abbcbbabbYZZ[]]^__abbcbbabbdeeCffdggdhhChhdhhijjkllmnnoppqppqppisjkllmnnoppqppqpptRRuTTuUUvWWuWWvWWwZZx]]y__zbb{bbzbbwZZx]]y__zbb{bbzbb|eeCffCggFhhChhFhh}jj~llÀnnÁppÂppÂpp}jj~llÀnnÁppÂppÂpp"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a} {z}","{h} {a} (sekunda: {s})","{h} {a} {z} (sekunda: {s})","{h} {a} (sekunda: {s1})","{h} {a} {z} (sekunda: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h2}","{h2} {z}","{h2} (sekunda: {s})","{h2} {z} (sekunda: {s})","{h2} (sekunda: {s1})","{h2} {z} (sekunda: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h}","{h} {z}","{s} (godzina: {h})","{h} {z} (sekunda: {s})","{h} (sekunda: {s1})","{h} {z} (sekunda: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[J]LM^_PQR"],"wt":{"l":[["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","36":"{E} {h} {a}"}],["{1}, {0}","{1}, {0}","{1}, {0}","{1}, {0}",{"30":"{E}, {h2}:{m}","34":"{E}, {h2}:{m}:{s}"}]],"s":0,"n":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"18":"{E} {h} {a}","24":"{E}, {h}:{m2} {a}","26":"{E}, {h}:{m2}:{s2} {a}","28":"{E}, {h}:{m2}:{s} {a}","30":"{E}, {h}:{m} {a}","32":"{E}, {h}:{m}:{s2} {a}","34":"{E}, {h}:{m}:{s} {a}","36":"{E} {h} {a}","42":"{E}, {h}:{m2} {a}","44":"{E}, {h}:{m2}:{s2} {a}","46":"{E}, {h}:{m2}:{s} {a}","48":"{E}, {h}:{m} {a}","50":"{E}, {h}:{m}:{s2} {a}","52":"{E}, {h}:{m}:{s} {a}"}],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"24":"{E}, {h2}:{m2}","26":"{E}, {h2}:{m2}:{s2}","28":"{E}, {h2}:{m2}:{s}","30":"{E}, {h2}:{m}","32":"{E}, {h2}:{m}:{s2}","34":"{E}, {h2}:{m}:{s}","42":"{E}, {h}:{m2}","44":"{E}, {h}:{m2}:{s2}","46":"{E}, {h}:{m2}:{s}","48":"{E}, {h}:{m}","50":"{E}, {h}:{m}:{s2}","52":"{E}, {h}:{m}:{s}"}]]},"g":{"f":"{1} {0}","l":"{1} {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {d1} {Ml} {y1}","{d1} {Ml} {y1}","{d1} {Ms} {y1}","{d1}.{M2}.{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} {0}","{1} {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","uniwersalny czas koordynowany"],"mz":{"5":["EET","EEST","czas wschodnioeuropejski standardowy","czas wschodnioeuropejski letni","czas wschodnioeuropejski"],"6":["CET","CEST","czas środkowoeuropejski standardowy","czas środkowoeuropejski letni","czas środkowoeuropejski"],"12":["","","Brasília (czas standardowy)","","Brasília (czas standardowy)"],"18":["","","czas górski standardowy","czas górski letni","czas górski"],"22":["","","czas środkowoamerykański standardowy","czas środkowoamerykański letni","czas środkowoamerykański"],"25":["","","czas wschodnioamerykański standardowy","czas wschodnioamerykański letni","czas wschodnioamerykański"],"35":["","","czas pacyficzny standardowy","czas pacyficzny letni","czas pacyficzny"],"45":["","","czas wschodnioaustralijski standardowy","czas wschodnioaustralijski letni","czas wschodnioaustralijski"],"61":["","","czas indyjski standardowy","","czas indyjski standardowy"],"77":["","","Chiny (czas standardowy)","","Chiny (czas standardowy)"],"82":["","","Korea (czas standardowy)","","Korea (czas standardowy)"],"90":["","","Japonia (czas standardowy)","","Japonia (czas standardowy)"],"111":["","","Moskwa (czas standardowy)","","Moskwa (czas standardowy)"],"112":["","","czas uniwersalny","Brytyjski czas letni","czas: Wielka Brytania"]},"zg":{},"rs":"–","fs":",","num":{"dec":",","grp":" ","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":2,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["# ¤","-# ¤"],"ca":["# ¤","(# ¤)"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","tys."],[3,"# C","tys."],[3,"# C","tys."],[6,"# C","mln"],[6,"# C","mln"],[6,"# C","mln"],[9,"# C","mld"],[9,"# C","mld"],[9,"# C","mld"],[12,"# C","bln"],[12,"# C","bln"],[12,"# C","bln"]],"cl":[[[3,"# C","tysiąc"],[3,"# C","tysiące"],[3,"# C","tysięcy"],[3,"# C","tysiąca"]],[3,"# C","tysięcy"],[3,"# C","tysięcy"],[[6,"# C","milion"],[6,"# C","miliony"],[6,"# C","milionów"],[6,"# C","miliona"]],[6,"# C","milionów"],[6,"# C","milionów"],[[9,"# C","miliard"],[9,"# C","miliardy"],[9,"# C","miliardów"],[9,"# C","miliarda"]],[9,"# C","miliardów"],[9,"# C","miliardów"],[[12,"# C","bilion"],[12,"# C","biliony"],[12,"# C","bilionów"],[12,"# C","biliona"]],[12,"# C","bilionów"],[12,"# C","bilionów"]],"cur":{"USD":[["dolar amerykański","dolary amerykańskie","dolarów amerykańskich","dolara amerykańskiego"],"USD"],"EUR":["euro"],"GBP":[["funt szterling","funty szterlingi","funtów szterlingów","funta szterlinga"],"GBP"],"JPY":[["jen japoński","jeny japońskie","jenów japońskich","jena japońskiego"],"JPY"],"CNY":[["juan chiński","juany chińskie","juanów chińskich","juana chińskiego"],"CNY"],"INR":[["rupia indyjska","rupie indyjskie","rupii indyjskich","rupii indyjskiej"],"INR"],"CAD":[["dolar kanadyjski","dolary kanadyjskie","dolarów kanadyjskich","dolara kanadyjskiego"],"CAD"],"AUD":[["dolar australijski","dolary australijskie","dolarów australijskich","dolara australijskiego"],"AUD"],"NZD":[0,"NZD"],"CHF":[["frank szwajcarski","franki szwajcarskie","franków szwajcarskich","franka szwajcarskiego"]],"SEK":[["korona szwedzka","korony szwedzkie","koron szwedzkich","korony szwedzkiej"]],"PLN":[["złoty polski","złote polskie","złotych polskich","złotego polskiego"],"zł"],"RUB":[["rubel rosyjski","ruble rosyjskie","rubli rosyjskich","rubla rosyjskiego"]],"BRL":[["real brazylijski","reale brazylijskie","reali brazylijskich","reala brazylijskiego"]],"MXN":[["peso meksykańskie","pesos meksykańskie","pesos meksykańskich","peso meksykańskiego"],"MXN"],"KRW":[["won południowokoreański","wony południowokoreańskie","wonów południowokoreańskich","wona południowokoreańskiego"],"KRW"],"HKD":[0,"HKD"],"TWD":[0,"TWD","NT$"],"ILS":[0,"ILS"],"PHP":[0,"PHP"],"VND":[0,"VND"]},"unit":{"byte":{"l":["{0} bajt","{0} bajty","{0} bajtów","{0} bajta"],"s":"{0} B","n":0},"kilobyte":{"l":["{0} kilobajt","{0} kilobajty","{0} kilobajtów","{0} kilobajta"],"s":"{0} kB","n":0},"megabyte":{"l":["{0} megabajt","{0} megabajty","{0} megabajtów","{0} megabajta"],"s":"{0} MB","n":0},"gigabyte":{"l":["{0} gigabajt","{0} gigabajty","{0} gigabajtów","{0} gigabajta"],"s":"{0} GB","n":0},"terabyte":{"l":["{0} terabajt","{0} terabajty","{0} terabajtów","{0} terabajta"],"s":"{0} TB","n":0},"percent":{"l":"{0} procent","s":"{0}%","n":0},"millisecond":{"l":["{0} milisekunda","{0} milisekundy","{0} milisekund","{0} milisekundy"],"s":"{0} ms","n":0},"second":{"l":["{0} sekunda","{0} sekundy","{0} sekund","{0} sekundy"],"s":"{0} sek.","n":"{0} s"},"minute":{"l":["{0} minuta","{0} minuty","{0} minut","{0} minuty"],"s":"{0} min","n":0},"hour":{"l":["{0} godzina","{0} godziny","{0} godzin","{0} godziny"],"s":"{0} godz.","n":"{0} h"},"day":{"l":["{0} dzień","{0} dni","{0} dni","{0} dnia"],"s":["{0} dzień","{0} dni","{0} dni","{0} dnia"],"n":"{0} d."},"week":{"l":["{0} tydzień","{0} tygodnie","{0} tygodni","{0} tygodnia"],"s":["{0} tydz.","{0} tyg.","{0} tyg.","{0} tyg."],"n":"{0} t."},"month":{"l":["{0} miesiąc","{0} miesiące","{0} miesięcy","{0} miesiąca"],"s":"{0} mies.","n":["{0} m-c","{0} m-ce","{0} m-cy","{0} m-ca"]},"year":{"l":["{0} rok","{0} lata","{0} lat","{0} roku"],"s":["{0} rok","{0} lata","{0} lat","{0} roku"],"n":["{0} r.","{0} l.","{0} l.","{0} r."]},"kilometer":{"l":["{0} kilometr","{0} kilometry","{0} kilometrów","{0} kilometra"],"s":"{0} km","n":0},"kilometer-per-hour":{"l":["{0} kilometr na godzinę","{0} kilometry na godzinę","{0} kilometrów na godzinę","{0} kilometra na godzinę"],"s":"{0} km/godz.","n":["{0} km/h","{0} km/h","{0} km/h","{0}km/h"]}},"rng":"{0}–{1}","approx":"~","rngS":""},"rel":{"l":{"year":[["za {0} rok","za {0} lata","za {0} lat","za {0} roku"],["{0} rok temu","{0} lata temu","{0} lat temu","{0} roku temu"],{"0":"w tym roku","1":"w przyszłym roku","-1":"w zeszłym roku"}],"quarter":[["za {0} kwartał","za {0} kwartały","za {0} kwartałów","za {0} kwartału"],["{0} kwartał temu","{0} kwartały temu","{0} kwartałów temu","{0} kwartału temu"],{"0":"w tym kwartale","1":"w przyszłym kwartale","-1":"w zeszłym kwartale"}],"month":[["za {0} miesiąc","za {0} miesiące","za {0} miesięcy","za {0} miesiąca"],["{0} miesiąc temu","{0} miesiące temu","{0} miesięcy temu","{0} miesiąca temu"],{"0":"w tym miesiącu","1":"w przyszłym miesiącu","-1":"w zeszłym miesiącu"}],"week":[["za {0} tydzień","za {0} tygodnie","za {0} tygodni","za {0} tygodnia"],["{0} tydzień temu","{0} tygodnie temu","{0} tygodni temu","{0} tygodnia temu"],{"0":"w tym tygodniu","1":"w przyszłym tygodniu","-1":"w zeszłym tygodniu"}],"day":[["za {0} dzień","za {0} dni","za {0} dni","za {0} dnia"],["{0} dzień temu","{0} dni temu","{0} dni temu","{0} dnia temu"],{"0":"dzisiaj","1":"jutro","2":"pojutrze","-2":"przedwczoraj","-1":"wczoraj"}],"hour":[["za {0} godzinę","za {0} godziny","za {0} godzin","za {0} godziny"],["{0} godzinę temu","{0} godziny temu","{0} godzin temu","{0} godziny temu"],{"0":"ta godzina"}],"minute":[["za {0} minutę","za {0} minuty","za {0} minut","za {0} minuty"],["{0} minutę temu","{0} minuty temu","{0} minut temu","{0} minuty temu"],{"0":"ta minuta"}],"second":[["za {0} sekundę","za {0} sekundy","za {0} sekund","za {0} sekundy"],["{0} sekundę temu","{0} sekundy temu","{0} sekund temu","{0} sekundy temu"],{"0":"teraz"}]},"s":{"year":[["za {0} rok","za {0} lata","za {0} lat","za {0} roku"],["{0} rok temu","{0} lata temu","{0} lat temu","{0} roku temu"],{"0":"w tym roku","1":"w przyszłym roku","-1":"w zeszłym roku"}],"quarter":["za {0} kw.","{0} kw. temu",{"0":"w tym kwartale","1":"w przyszłym kwartale","-1":"w zeszłym kwartale"}],"month":["za {0} mies.","{0} mies. temu",{"0":"w tym mies.","1":"w przyszłym mies.","-1":"w zeszłym mies."}],"week":[["za {0} tydz.","za {0} tyg.","za {0} tyg.","za {0} tyg."],["{0} tydz. temu","{0} tyg. temu","{0} tyg. temu","{0} tyg. temu"],{"0":"w tym tyg.","1":"w przyszłym tyg.","-1":"w zeszłym tyg."}],"day":[["za {0} dzień","za {0} dni","za {0} dni","za {0} dnia"],["{0} dzień temu","{0} dni temu","{0} dni temu","{0} dnia temu"],{"0":"dzisiaj","1":"jutro","2":"pojutrze","-2":"przedwczoraj","-1":"wczoraj"}],"hour":["za {0} godz.","{0} godz. temu",{"0":"ta godzina"}],"minute":["za {0} min","{0} min temu",{"0":"ta minuta"}],"second":["za {0} sek.","{0} sek. temu",{"0":"teraz"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":[["za {0} dzień","za {0} dni","za {0} dni","za {0} dnia"],["{0} dzień temu","{0} dni temu","{0} dni temu","{0} dnia temu"],{"0":"dziś","1":"jutro","2":"pojutrze","-2":"przedwczoraj","-1":"wcz."}],"hour":["za {0} g.","{0} g. temu",{"0":"ta godzina"}],"minute":0,"second":["za {0} s","{0} s temu",{"0":"teraz"}]}},"list":{"cl":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"],"cs":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"],"cn":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"],"dl":["{0} lub {1}","{0}, {1}","{0}, {1}","{0} lub {1}"],"ds":["{0} lub {1}","{0}, {1}","{0}, {1}","{0} lub {1}"],"dn":["{0} lub {1}","{0}, {1}","{0}, {1}","{0} lub {1}"],"ul":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"],"us":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"],"un":["{0} i {1}","{0}, {1}","{0}, {1}","{0} i {1}"]},"dn":{"fld":{"l":["era","rok","kwartał","miesiąc","tydzień","dzień tygodnia","dzień","rano / po południu / wieczorem","godzina","minuta","sekunda","strefa czasowa"],"s":["era","r.","kw.","mies.","tydz.","dzień tyg.","dz.","rano / po południu / wieczorem","godz.","min","sek.","str. czasowa"],"n":["era","r.","kw.","mc","tydz.","dz. tyg.","d.","rano / po poł. / wiecz.","g.","min","s","str. czas."]},"lp":["{0} ({1})",", "]}}`,
    "ja": `{"n":{"Ml":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ll":0,"El":["日曜日","月曜日","火曜日","水曜日","木曜日","金曜日","土曜日"],"cl":0,"Gl":["紀元前","西暦"],"Bl":[0,"夜中",4,"朝",12,"正午",13,"昼",16,"夕方",19,"夜",23,"夜中"],"Bl0":["夜中","正午"],"Ms":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ls":0,"Es":["日","月","火","水","木","金","土"],"cs":0,"Gs":["紀元前","西暦"],"Bs":[0,"夜中",4,"朝",12,"正午",13,"昼",16,"夕方",19,"夜",23,"夜中"],"Bs0":["夜中","正午"],"Mn":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ln":0,"En":["日","月","火","水","木","金","土"],"cn":0,"Gn":["BC","AD"],"Bn":[0,"夜中",4,"朝",12,"正午",13,"昼",16,"夕方",19,"夜",23,"夜中"],"Bn0":["夜中","正午"],"a":["午前","午後"]},"dp":[["","{d}日","{M}月","{M}/{d}","{M1}月","{M1}月{d}日","{y}年","{y}年 (日: {d}日)","{y}/{M}","{y}/{M}/{d}","{y}年{M1}月","{y}年{M1}月{d}日","{y}年 {d}日","{G}{y1}/{M1}/{d1}","{G} (日: {d}日)","{G} (月: {M}月)","{G} {M}/{d}","{G} (月: {M1}月)","{G} {M1}月{d}日","{G}{y}年","{G}{y}年 (日: {d}日)","{G}{y}/{M}","{G}{y}/{M}/{d}","{G}{y}年{M1}月","{G}{y}年{M1}月{d}日","{E}","{d}日{E}","{M}月 {E}","{M}/{d}{E}","{M1}月 {E}","{M1}月{d}日{E}","{E} (月: {M1}月)","{y}年 {E}","{y}年 {d}日{E}","{y}/{M} {E}","{y}/{M}/{d}{E}","{y}年{M1}月 {E}","{y}年{M1}月{d}日{E}","{G} {E}","{G} {d}日{E}","{G} {M}/{d}{E}","{G} {M1}月{d}日{E}","{G}{y}年 {E}","{G}{y}年 (日: {d}日{E})","{G}{y}/{M} {E}","{G}{y}/{M}/{d}({E})","{G}{y}年{M1}月 {E}","{G}{y}年{M1}月{d}日{E}","{G}{y}年 {d}日{E}","{d}日({E})","{M}/{d}({E})","{E} (月: {M}月)","{M1}月{d}日({E})","{y}年 {d}日({E})","{y}/{M}/{d}({E})","{y}年{M1}月{d}日({E})","{G} {d}日({E})","{G} {M}/{d}({E})","{G} {M1}月{d}日({E})","{G}{y}年 (日: {d}日({E}))","{G}{y}年{M1}月{d}日({E})","{G}{y}年 {d}日({E})"],"011233233455455455677899899:;;:;;:;;6<7899899:;;:;;:;;=>>?@@?@@ABBABBABBCDDEFFEFFGHHGHHGHHCDDEFFEFFGHHGHHGHHIJJKLLKLLMNNMNNONNPQQRSSRSSTUUTUUTUUPQQRSSRSSTUUTUUTUUVWW?XX?XXAYYAYYVYYZ[[]^^]^^_aa_aa_aaZb[]^^]^^_aa_aa_aaIccKddeddOffMffOffPggRhhRhhTiiTiiTiiPggRhhRhhTiiTiiTiiVjj?kkVkkVllAllVllZmm]^^]^^_nn_nn_nnZom]^^]^^_nn_nn_nnIccKddKddMffMffMffPggRhhRhhTiiTiiTiiPggRhhRhhTiiTiiTiiVjj?kk?kkAllAllAllZmm]^^]^^_nn_nn_nnZmm]^^]^^_nn_nn_nn"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{a}{h}時","{a}{h}時 {z}","{a}{h}時 (秒: {s})","{a}{h}時 {z} (秒: {s})","{a}{h}時 (秒: {s1})","{a}{h}時 {z} (秒: {s1})","{a}{h}:{m2}","{a}{h}:{m2} {z}","{a}{h}:{m2}:{s2}","{a}{h}:{m2}:{s2} {z}","{a}{h}:{m2}:{s}","{a}{h}:{m2}:{s} {z}","{a}{h}:{m}","{a}{h}:{m} {z}","{a}{h}:{m}:{s2}","{a}{h}:{m}:{s2} {z}","{a}{h}:{m}:{s}","{a}{h}:{m}:{s} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h}時","{h}時 {z}","{h}時 (秒: {s})","{h}時 {z} (秒: {s})","{h}時 (秒: {s1})","{h}時 {z} (秒: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h}:{m} {z}","{h}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{s} (時: {h}時)"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABSDEFGHIJKLMNOPQR"],"wt":{"l":[["{1} {0}","{0} ({1})","{1} {0}","{1} {0}",{"18":"{a}{h}時 ({E})","36":"{a}{h}時 ({E})"}],["{1} {0}","{0} ({1})","{1} {0}","{1} {0}"]],"s":0,"n":0},"g":{"f":"{1} {0}","l":"{1} {0}","m":"{1} {0}","s":"{1} {0}"},"st":{"d":["{y1}年{M1}月{d1}日{El}","{y1}年{M1}月{d1}日","{y1}/{M2}/{d2}","{y1}/{M2}/{d2}"],"t12":["{a}{h1}:{m2}:{s2} {zl}","{a}{h1}:{m2}:{s2} {zs}","{a}{h1}:{m2}:{s2}","{a}{h1}:{m2}"],"t23":["{h1}時{m2}分{s2}秒 {zl}","{h1}:{m2}:{s2} {zs}","{h1}:{m2}:{s2}","{h1}:{m2}"],"g":["{1} {0}","{1} {0}","{1} {0}","{1} {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","協定世界時"],"mz":{"5":["","","東ヨーロッパ標準時","東ヨーロッパ夏時間","東ヨーロッパ時間"],"6":["","","中央ヨーロッパ標準時","中央ヨーロッパ夏時間","中央ヨーロッパ時間"],"12":["","","ブラジリア標準時","","ブラジリア標準時"],"18":["","","米国山岳標準時","米国山岳夏時間","米国山岳部時間"],"22":["","","米国中部標準時","米国中部夏時間","米国中部時間"],"25":["","","米国東部標準時","米国東部夏時間","米国東部時間"],"35":["","","米国太平洋標準時","米国太平洋夏時間","米国太平洋時間"],"45":["","","オーストラリア東部標準時","オーストラリア東部夏時間","オーストラリア東部時間"],"61":["","","インド標準時","","インド標準時"],"77":["","","中国標準時","","中国標準時"],"82":["","","韓国標準時","","韓国標準時"],"90":["JST","","日本標準時","","日本標準時"],"111":["","","モスクワ標準時","","モスクワ標準時"],"112":["","","グリニッジ標準時","英国夏時間","イギリス時間"]},"zg":{},"rs":"～","fs":".","num":{"dec":".","grp":",","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤#","-¤#"],"ca":["¤#","(¤#)"],"cc":["¤ #","-¤ #"],"cn":["#¤","-#¤"]},"cs":[null,[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[8,"#C","億"],[8,"#C","億"],[8,"#C","億"],[8,"#C","億"],[12,"#C","兆"],[12,"#C","兆"],[12,"#C","兆"]],"cl":[null,[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[8,"#C","億"],[8,"#C","億"],[8,"#C","億"],[8,"#C","億"],[12,"#C","兆"],[12,"#C","兆"],[12,"#C","兆"]],"cur":{"USD":["米ドル"],"EUR":["ユーロ"],"GBP":["英国ポンド"],"JPY":["円","￥","￥"],"CNY":["中国人民元","元","￥"],"INR":["インド ルピー"],"CAD":["カナダ ドル"],"AUD":["オーストラリア ドル"],"CHF":["スイス フラン"],"SEK":["スウェーデン クローナ"],"PLN":["ポーランド ズウォティ"],"RUB":["ロシア ルーブル"],"BRL":["ブラジル レアル"],"MXN":["メキシコ ペソ"],"KRW":["韓国ウォン"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":"{0} バイト","s":"{0} byte","n":"{0}B"},"kilobyte":{"l":"{0} キロバイト","s":"{0} KB","n":"{0}KB"},"megabyte":{"l":"{0} メガバイト","s":"{0} MB","n":"{0}MB"},"gigabyte":{"l":"{0} ギガバイト","s":"{0} GB","n":"{0}GB"},"terabyte":{"l":"{0} テラバイト","s":"{0} TB","n":"{0}TB"},"percent":{"l":"{0} パーセント","s":"{0}%","n":0},"millisecond":{"l":"{0} ミリ秒","s":"{0} ms","n":"{0}ms"},"second":{"l":"{0} 秒","s":"{0} 秒","n":"{0}s"},"minute":{"l":"{0} 分","s":"{0} 分","n":"{0}m"},"hour":{"l":"{0} 時間","s":"{0} 時間","n":"{0}h"},"day":{"l":"{0} 日","s":"{0} 日","n":"{0}d"},"week":{"l":"{0} 週間","s":"{0} 週間","n":"{0}w"},"month":{"l":"{0} か月","s":"{0} か月","n":"{0}m"},"year":{"l":"{0} 年","s":"{0} 年","n":"{0}y"},"kilometer":{"l":"{0} キロメートル","s":"{0} km","n":"{0}km"},"kilometer-per-hour":{"l":"時速 {0} キロメートル","s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}～{1}","approx":"約","rngS":" ～ "},"rel":{"l":{"year":["{0} 年後","{0} 年前",{"0":"今年","1":"来年","-1":"昨年"}],"quarter":["{0} 四半期後","{0} 四半期前",{"0":"今四半期","1":"翌四半期","-1":"前四半期"}],"month":["{0} か月後","{0} か月前",{"0":"今月","1":"来月","-1":"先月"}],"week":["{0} 週間後","{0} 週間前",{"0":"今週","1":"来週","-1":"先週"}],"day":["{0} 日後","{0} 日前",{"0":"今日","1":"明日","2":"明後日","-2":"一昨日","-1":"昨日"}],"hour":["{0} 時間後","{0} 時間前",{"0":"1 時間以内"}],"minute":["{0} 分後","{0} 分前",{"0":"1 分以内"}],"second":["{0} 秒後","{0} 秒前",{"0":"今"}]},"s":{"year":["{0} 年後","{0} 年前",{"0":"今年","1":"来年","-1":"昨年"}],"quarter":["{0} 四半期後","{0} 四半期前",{"0":"今四半期","1":"翌四半期","-1":"前四半期"}],"month":["{0} か月後","{0} か月前",{"0":"今月","1":"来月","-1":"先月"}],"week":["{0} 週間後","{0} 週間前",{"0":"今週","1":"来週","-1":"先週"}],"day":["{0} 日後","{0} 日前",{"0":"今日","1":"明日","2":"明後日","-2":"一昨日","-1":"昨日"}],"hour":["{0} 時間後","{0} 時間前",{"0":"1 時間以内"}],"minute":["{0} 分後","{0} 分前",{"0":"1 分以内"}],"second":["{0} 秒後","{0} 秒前",{"0":"今"}]},"n":{"year":["{0}年後","{0}年前",{"0":"今年","1":"来年","-1":"昨年"}],"quarter":["{0}四半期後","{0}四半期前",{"0":"今四半期","1":"翌四半期","-1":"前四半期"}],"month":["{0}か月後","{0}か月前",{"0":"今月","1":"来月","-1":"先月"}],"week":["{0}週間後","{0}週間前",{"0":"今週","1":"来週","-1":"先週"}],"day":["{0}日後","{0}日前",{"0":"今日","1":"明日","2":"明後日","-2":"一昨日","-1":"昨日"}],"hour":["{0}時間後","{0}時間前",{"0":"1 時間以内"}],"minute":["{0}分後","{0}分前",{"0":"1 分以内"}],"second":["{0}秒後","{0}秒前",{"0":"今"}]}},"list":{"cl":["{0}、{1}","{0}、{1}","{0}、{1}","{0}、{1}"],"cs":["{0}、{1}","{0}、{1}","{0}、{1}","{0}、{1}"],"cn":["{0}、{1}","{0}、{1}","{0}、{1}","{0}、{1}"],"dl":["{0}または{1}","{0}、{1}","{0}、{1}","{0}、または{1}"],"ds":["{0}または{1}","{0}、{1}","{0}、{1}","{0}、または{1}"],"dn":["{0}または{1}","{0}、{1}","{0}、{1}","{0}、または{1}"],"ul":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"us":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"un":["{0}{1}","{0}{1}","{0}{1}","{0}{1}"]},"dn":{"lang":{"de-AT":"ドイツ語 (オーストリア)","de-CH":"標準ドイツ語 (スイス)","en-AU":"オーストラリア英語","en-CA":"カナダ英語","fr-CH":"フランス語 (スイス)","nl-BE":"フラマン語","es-ES":"スペイン語 (イベリア半島)","yue":"広東語","haw":"ハワイ語","tlh":"クリンゴン語","sr":"セルビア語","hr":"クロアチア語","sk":"スロバキア語","sl":"スロベニア語","bg":"ブルガリア語","lt":"リトアニア語","lv":"ラトビア語","et":"エストニア語","is":"アイスランド語","ka":"ジョージア語","hy":"アルメニア語","az":"アゼルバイジャン語","kk":"カザフ語","ne":"ネパール語","si":"シンハラ語","km":"クメール語","lo":"ラオ語","my":"ミャンマー語","am":"アムハラ語","zu":"ズールー語","af":"アフリカーンス語","gl":"ガリシア語","eu":"バスク語","cy":"ウェールズ語","mt":"マルタ語","en":"英語","es":"スペイン語","fr":"フランス語","de":"ドイツ語","it":"イタリア語","pt":"ポルトガル語","nl":"オランダ語","sv":"スウェーデン語","pl":"ポーランド語","ru":"ロシア語","uk":"ウクライナ語","ja":"日本語","zh":"中国語","ko":"韓国語","ar":"アラビア語","he":"ヘブライ語","hi":"ヒンディー語","bn":"ベンガル語","ur":"ウルドゥー語","fa":"ペルシア語","tr":"トルコ語","el":"ギリシャ語","cs":"チェコ語","da":"デンマーク語","fi":"フィンランド語","nb":"ノルウェー語(ブークモール)","no":"ノルウェー語","hu":"ハンガリー語","ro":"ルーマニア語","id":"インドネシア語","ms":"マレー語","th":"タイ語","vi":"ベトナム語","tl":"フィリピノ語","fil":"フィリピノ語","ta":"タミル語","te":"テルグ語","sw":"スワヒリ語","ca":"カタロニア語","eo":"エスペラント語","la":"ラテン語","ga":"アイルランド語","zh-Hans":"簡体中国語","zh-Hant":"繁体中国語","en-US":"アメリカ英語","en-GB":"イギリス英語","pt-BR":"ポルトガル語 (ブラジル)","pt-PT":"ポルトガル語 (イベリア半島)","es-419":"スペイン語 (ラテンアメリカ)","es-MX":"スペイン語 (メキシコ)","fr-CA":"フランス語 (カナダ)"},"langS":{"az":"アゼリー語","en-US":"英語(米国)","en-GB":"英語(英国)"},"reg":{"142":"アジア","150":"ヨーロッパ","419":"ラテンアメリカ","US":"アメリカ合衆国","GB":"イギリス","CA":"カナダ","AU":"オーストラリア","NZ":"ニュージーランド","IE":"アイルランド","IN":"インド","PK":"パキスタン","BD":"バングラデシュ","DE":"ドイツ","AT":"オーストリア","CH":"スイス","FR":"フランス","BE":"ベルギー","LU":"ルクセンブルク","ES":"スペイン","PT":"ポルトガル","IT":"イタリア","NL":"オランダ","SE":"スウェーデン","NO":"ノルウェー","DK":"デンマーク","FI":"フィンランド","IS":"アイスランド","PL":"ポーランド","CZ":"チェコ","SK":"スロバキア","HU":"ハンガリー","RO":"ルーマニア","BG":"ブルガリア","GR":"ギリシャ","TR":"トルコ","RU":"ロシア","UA":"ウクライナ","BY":"ベラルーシ","IL":"イスラエル","EG":"エジプト","SA":"サウジアラビア","AE":"アラブ首長国連邦","IR":"イラン","ZA":"南アフリカ","NG":"ナイジェリア","KE":"ケニア","MX":"メキシコ","BR":"ブラジル","AR":"アルゼンチン","CL":"チリ","CO":"コロンビア","PE":"ペルー","VE":"ベネズエラ","CU":"キューバ","JP":"日本","CN":"中国","TW":"台湾","HK":"中華人民共和国香港特別行政区","KR":"韓国","SG":"シンガポール","MY":"マレーシア","ID":"インドネシア","TH":"タイ","VN":"ベトナム","PH":"フィリピン","XK":"コソボ","RS":"セルビア","HR":"クロアチア","SI":"スロベニア","BA":"ボスニア・ヘルツェゴビナ","AL":"アルバニア","LT":"リトアニア","LV":"ラトビア","EE":"エストニア","GE":"ジョージア","KZ":"カザフスタン","EU":"欧州連合","UN":"国際連合","ZZ":"不明な地域","AQ":"南極","001":"世界","002":"アフリカ","019":"アメリカ大陸"},"regS":{"US":"アメリカ","GB":"英国","HK":"香港","UN":"国連"},"cur":{"USD":"米ドル","EUR":"ユーロ","GBP":"英国ポンド","JPY":"日本円","CNY":"中国人民元","INR":"インド ルピー","CAD":"カナダ ドル","AUD":"オーストラリア ドル","CHF":"スイス フラン","BRL":"ブラジル レアル","MXN":"メキシコ ペソ","KRW":"韓国ウォン","RUB":"ロシア ルーブル","SEK":"スウェーデン クローナ","PLN":"ポーランド ズウォティ","XXX":"不明または無効な通貨","HKD":"香港ドル","TWD":"新台湾ドル","NZD":"ニュージーランド ドル","NOK":"ノルウェー クローネ","DKK":"デンマーク クローネ","CZK":"チェコ コルナ","HUF":"ハンガリー フォリント","TRY":"トルコ リラ","ZAR":"南アフリカ ランド","SGD":"シンガポール ドル","ILS":"イスラエル新シェケル","AED":"アラブ首長国連邦ディルハム"},"scr":{"Latn":"ラテン文字","Cyrl":"キリル文字","Arab":"アラビア文字","Hans":"簡体字","Hant":"繁体字","Jpan":"日本語の文字","Kore":"韓国語の文字","Grek":"ギリシャ文字","Hebr":"ヘブライ文字","Deva":"デーバナーガリー文字"},"cal":{"buddhist":"仏暦","chinese":"中国暦","coptic":"コプト暦","dangi":"ダンギ暦","ethioaa":"エチオピア創世紀元暦","ethiopic":"エチオピア暦","gregory":"西暦(グレゴリオ暦)","hebrew":"ユダヤ暦","indian":"インド国定暦","islamic":"イスラム暦","islamic-civil":"イスラム暦(定周期、公民紀元)","islamic-umalqura":"イスラム暦(ウンム・アルクラー)","iso8601":"ISO-8601","japanese":"和暦","persian":"ペルシア暦","roc":"中華民国暦"},"fld":{"l":["時代","年","四半期","月","週","曜日","日","午前/午後","時","分","秒","タイムゾーン"],"s":["時代","年","四半期","月","週","曜日","日","午前/午後","時","分","秒","タイムゾーン"],"n":0},"lp":["{0} ({1})","、"]}}`,
    "zh": `{"n":{"Ml":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ll":["一月","二月","三月","四月","五月","六月","七月","八月","九月","十月","十一月","十二月"],"El":["星期日","星期一","星期二","星期三","星期四","星期五","星期六"],"cl":0,"Gl":["公元前","公元"],"Bl":[0,"凌晨",5,"清晨",8,"上午",12,"中午",13,"下午",19,"晚上"],"Bl0":["凌晨","中午"],"Ms":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ls":["1月","2月","3月","4月","5月","6月","7月","8月","9月","10月","11月","12月"],"Es":["周日","周一","周二","周三","周四","周五","周六"],"cs":0,"Gs":["公元前","公元"],"Bs":[0,"凌晨",5,"早上",8,"上午",12,"中午",13,"下午",19,"晚上"],"Bs0":["凌晨","中午"],"Mn":["1","2","3","4","5","6","7","8","9","10","11","12"],"Ln":0,"En":["日","一","二","三","四","五","六"],"cn":0,"Gn":["公元前","公元"],"Bn":[0,"凌晨",5,"早上",8,"上午",12,"中午",13,"下午",19,"晚上"],"Bn0":["凌晨","中午"],"a":["上午","下午"]},"dp":[["","{d}日","{M}月","{M}/{d}","{L}","{M1}月{d}日","{M1}","{y}年","{y}年 (日: {d}日)","{y}/{M}","{y}/{M}/{d}","{y}年{M1}月","{y}年{M1}月{d}日","{y}年 {d}日","{G}{y1}-{M2}-{d2}","{G} (日: {d}日)","{G} (月: {M}月)","{G} {M}/{d}","{G} (月: {L})","{G} {M1}月{d}日","{G} (月: {M1})","{G}{y}年","{G}{y}年 (日: {d}日)","{G}{y}年{M}月","{G}{y}-{M2}-{d2}","{G}{y}-{M2}-{d}","{G}{y}-{M}-{d2}","{G}{y}-{M}-{d}","{G}{y}年{M1}月","{G}{y}年{M1}月{d}日","{E}","{d}日{E}","{M}月 {E}","{M}/{d}{E}","{L} {E}","{M1}月{d}日{E}","{E} (月: {M1})","{y}年 {E}","{y}年 {d}日{E}","{y}/{M} {E}","{y}年{M}月{d}日{E}","{y}年{M1}月 {E}","{y}年{M1}月{d}日{E}","{G} {E}","{G} {d}日{E}","{G} {M}/{d}{E}","{G} {M1}月{d}日{E}","{G}{y}年 {E}","{G}{y}年 (日: {d}日{E})","{G}{y}年{M}月 {E}","{G}{y}-{M2}-{d2}{E}","{G}{y}-{M2}-{d}{E}","{G}{y}-{M}-{d2}{E}","{G}{y}-{M}-{d}{E}","{G}{y}年{M1}月 {E}","{G}{y}年{M1}月{d}日{E}","{E} (月: {M}月)","{E} (月: {L})","{y}/{M}/{d}{E}","{G}{y}年 {d}日{E}","{M1} {E}"],"0112332334554556557889::;::;<<;<<;<<7=89::;::;<<;<<;<<>??@AA@AABCCBCCDCCEFFGHIGJKLMMLMMLMMEFFGHIGJKLMMLMMLMMNOOPQQPQQRSSRSSTSSUVVWXXYXXYZZYZZYZZUVVWXXYXXYZZYZZYZZ[]]@^^@^^B__B__[__abbcdecfghiihiihiiabbcdecfghiihiihiiNOOPQQjQQkSSRSSTSSUVVWllYllYZZYZZYZZUVVWllYllYZZYZZYZZ[]]@^^[^^[__B__[__abbcdecfghiihiihiiambcdecfghiihiihiiNOOPQQPQQRSSRSSnSSUVVWllYllYZZYZZYZZUVVWllYllYZZYZZYZZ[]]@^^@^^B__B__D__abbcdecfghiihiihiiabbcdecfghiihiihii"],"tp12":[["","{s}","{z}{s}","{s1}","{z}{s1}","{m}","{z}{m}","{m2}:{s2}","{z}{m2}:{s2}","{m2}:{s}","{z}{m2}:{s}","{m1}","{z}{m1}","{m}:{s2}","{z}{m}:{s2}","{m}:{s}","{z}{m}:{s}","{a}{h}时","{z}{a}{h}时","{a}{h}时 (秒: {s})","{z}{a}{h}时 (秒: {s})","{a}{h}时 (秒: {s1})","{z}{a}{h}时 (秒: {s1})","{a}{h}:{m2}","{z}{a}{h}:{m2}","{a}{h}:{m2}:{s2}","{z}{a}{h}:{m2}:{s2}","{a}{h}:{m2}:{s}","{z}{a}{h}:{m2}:{s}","{a}{h}:{m}","{z}{a}{h}:{m}","{a}{h}:{m}:{s2}","{z}{a}{h}:{m}:{s2}","{a}{h}:{m}:{s}","{z}{a}{h}:{m}:{s}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{z}{s}","{s1}","{z}{s1}","{m}","{z}{m}","{m2}:{s2}","{z}{m2}:{s2}","{m2}:{s}","{z}{m2}:{s}","{m1}","{z}{m1}","{m}:{s2}","{z}{m}:{s2}","{m}:{s}","{z}{m}:{s}","{h}时","{z}{h}时","{h}时 (秒: {s})","{z}{h}时 (秒: {s})","{h}时 (秒: {s1})","{z}{h}时 (秒: {s1})","{h2}:{m2}","{z} {h2}:{m2}","{h2}:{m2}:{s2}","{z} {h}:{m2}:{s2}","{h2}:{m2}:{s}","{z} {h}:{m2}:{s}","{h}:{m}","{z} {h2}:{m}","{h2}:{m}:{s2}","{z} {h}:{m}:{s2}","{h}:{m}:{s}","{z} {h}:{m}:{s}","{s} (小时: {h}时)","{h}:{m2}","{z} {h}:{m2}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{z} {h}:{m}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABSDEFTUVJWLMXYPQR"],"wt":{"l":[["{1} {0}","{1}{0}","{1} {0}","{1} {0}",{"18":"{E}{a}{h}时","36":"{E}{a}{h}时"}],["{1} {0}","{1}{0}","{1} {0}","{1} {0}",{"30":"{E}{h2}:{m}","34":"{E}{h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} {0}","l":"{1} {0}","m":"{1} {0}","s":"{1} {0}"},"st":{"d":["{y1}年{M1}月{d1}日{El}","{y1}年{M1}月{d1}日","{y1}年{M1}月{d1}日","{y1}/{M1}/{d1}"],"t12":["{zl}{a}{h2}:{m2}:{s2}","{zs}{a}{h2}:{m2}:{s2}","{a}{h2}:{m2}:{s2}","{a}{h2}:{m2}"],"t23":["{zl} {h2}:{m2}:{s2}","{zs} {h2}:{m2}:{s2}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} {0}","{1} {0}","{1} {0}","{1} {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","协调世界时"],"mz":{"5":["","","东欧标准时间","东欧夏令时间","东欧时间"],"6":["","","中欧标准时间","中欧夏令时间","中欧时间"],"12":["","","巴西利亚标准时间","","巴西利亚标准时间"],"18":["","","北美山区标准时间","北美山区夏令时间","北美山区时间"],"22":["","","北美中部标准时间","北美中部夏令时间","北美中部时间"],"25":["","","北美东部标准时间","北美东部夏令时间","北美东部时间"],"35":["","","北美太平洋标准时间","北美太平洋夏令时间","北美太平洋时间"],"45":["","","澳大利亚东部标准时间","澳大利亚东部夏令时间","澳大利亚东部时间"],"61":["","","印度时间","","印度时间"],"77":["","","中国标准时间","","中国标准时间"],"82":["","","韩国标准时间","","韩国标准时间"],"90":["","","日本标准时间","","日本标准时间"],"111":["","","莫斯科标准时间","","莫斯科标准时间"],"112":["","","格林尼治标准时间","英国夏令时间","英国时间"]},"zg":{},"rs":"–","fs":".","num":{"dec":".","grp":",","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤#","-¤#"],"ca":["¤#","(¤#)"],"cc":["¤ #","-¤ #"],"cn":["#¤","-#¤"]},"cs":[null,[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[8,"#C","亿"],[8,"#C","亿"],[8,"#C","亿"],[8,"#C","亿"],[12,"#C","万亿"],[12,"#C","万亿"],[12,"#C","万亿"]],"cl":[null,[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[4,"#C","万"],[8,"#C","亿"],[8,"#C","亿"],[8,"#C","亿"],[8,"#C","亿"],[12,"#C","万亿"],[12,"#C","万亿"],[12,"#C","万亿"]],"cur":{"USD":["美元","US$"],"EUR":["欧元"],"GBP":["英镑"],"JPY":["日元","JP¥"],"CNY":["人民币","¥"],"INR":["印度卢比"],"CAD":["加拿大元"],"AUD":["澳大利亚元","AU$"],"CHF":["瑞士法郎"],"SEK":["瑞典克朗"],"PLN":["波兰兹罗提"],"RUB":["俄罗斯卢布"],"BRL":["巴西雷亚尔"],"MXN":["墨西哥比索"],"KRW":["韩元"],"TWD":[0,0,"NT$"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":"{0}字节","s":"{0} B","n":0},"kilobyte":{"l":"{0}千字节","s":"{0} kB","n":0},"megabyte":{"l":"{0}兆字节","s":"{0} MB","n":0},"gigabyte":{"l":"{0}吉字节","s":"{0} GB","n":0},"terabyte":{"l":"{0}太字节","s":"{0} TB","n":0},"percent":{"l":"{0}%","s":"{0}%","n":0},"millisecond":{"l":"{0}毫秒","s":"{0}毫秒","n":"{0}ms"},"second":{"l":"{0}秒钟","s":"{0}秒","n":0},"minute":{"l":"{0}分钟","s":"{0}分钟","n":0},"hour":{"l":"{0}小时","s":"{0}小时","n":0},"day":{"l":"{0}天","s":"{0}天","n":0},"week":{"l":"{0}周","s":"{0}周","n":0},"month":{"l":"{0}个月","s":"{0}个月","n":0},"year":{"l":"{0}年","s":"{0}年","n":0},"kilometer":{"l":"{0}公里","s":"{0}公里","n":0},"kilometer-per-hour":{"l":"每小时{0}公里","s":"{0} km/h","n":"{0}km/h"}},"rng":"{0}-{1}","approx":"~","rngS":" - "},"rel":{"l":{"year":["{0}年后","{0}年前",{"0":"今年","1":"明年","-1":"去年"}],"quarter":["{0}个季度后","{0}个季度前",{"0":"本季度","1":"下季度","-1":"上季度"}],"month":["{0}个月后","{0}个月前",{"0":"本月","1":"下个月","-1":"上个月"}],"week":["{0}周后","{0}周前",{"0":"本周","1":"下周","-1":"上周"}],"day":["{0}天后","{0}天前",{"0":"今天","1":"明天","2":"后天","-2":"前天","-1":"昨天"}],"hour":["{0}小时后","{0}小时前",{"0":"这一时间 / 此时"}],"minute":["{0}分钟后","{0}分钟前",{"0":"此刻"}],"second":["{0}秒钟后","{0}秒钟前",{"0":"现在"}]},"s":{"year":["{0}年后","{0}年前",{"0":"今年","1":"明年","-1":"去年"}],"quarter":["{0}个季度后","{0}个季度前",{"0":"本季度","1":"下季度","-1":"上季度"}],"month":["{0}个月后","{0}个月前",{"0":"本月","1":"下个月","-1":"上个月"}],"week":["{0}周后","{0}周前",{"0":"本周","1":"下周","-1":"上周"}],"day":["{0}天后","{0}天前",{"0":"今天","1":"明天","2":"后天","-2":"前天","-1":"昨天"}],"hour":["{0}小时后","{0}小时前",{"0":"这一时间 / 此时"}],"minute":["{0}分钟后","{0}分钟前",{"0":"此刻"}],"second":["{0}秒后","{0}秒前",{"0":"现在"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0}和{1}","{0}、{1}","{0}、{1}","{0}和{1}"],"cs":["{0}和{1}","{0}、{1}","{0}、{1}","{0}和{1}"],"cn":["{0}、{1}","{0}、{1}","{0}、{1}","{0}、{1}"],"dl":["{0}或{1}","{0}、{1}","{0}、{1}","{0}或{1}"],"ds":["{0}或{1}","{0}、{1}","{0}、{1}","{0}或{1}"],"dn":["{0}或{1}","{0}、{1}","{0}、{1}","{0}或{1}"],"ul":["{0}{1}","{0}{1}","{0}{1}","{0}{1}"],"us":["{0}{1}","{0}{1}","{0}{1}","{0}{1}"],"un":["{0}{1}","{0}{1}","{0}{1}","{0}{1}"]},"dn":{"fld":{"l":["纪元","年","季度","月","周","星期","日","上午/下午","小时","分钟","秒","时区"],"s":["纪元","年","季","月","周","星期","日","上午/下午","小时","分","秒","时区"],"n":0},"lp":["{0}（{1}）","，"]}}`,
    "ko": `{"n":{"Ml":["1월","2월","3월","4월","5월","6월","7월","8월","9월","10월","11월","12월"],"Ll":0,"El":["일요일","월요일","화요일","수요일","목요일","금요일","토요일"],"cl":0,"Gl":["기원전","서기"],"Bl":[0,"밤",3,"아침",6,"오전",12,"정오",13,"오후",18,"저녁",21,"밤"],"Bl0":["밤","정오"],"Ms":["1월","2월","3월","4월","5월","6월","7월","8월","9월","10월","11월","12월"],"Ls":0,"Es":["일","월","화","수","목","금","토"],"cs":0,"Gs":["BC","AD"],"Bs":[0,"밤",3,"아침",6,"오전",12,"정오",13,"오후",18,"저녁",21,"밤"],"Bs0":["밤","정오"],"Mn":["1월","2월","3월","4월","5월","6월","7월","8월","9월","10월","11월","12월"],"Ln":0,"En":["일","월","화","수","목","금","토"],"cn":0,"Gn":["BC","AD"],"Bn":[0,"밤",3,"아침",6,"오전",12,"정오",13,"오후",18,"저녁",21,"밤"],"Bn0":["밤","정오"],"a":["오전","오후"]},"dp":[["","{d}일","{M}월","{M}. {d}.","{M}","{M} {d}일","{y}년","{y}년 (일: {d}일)","{y}. {M}.","{y}. {M}. {d}.","{y}. {M1}.","{y}년 {M}","{y}년 {M} {d}일","{y}년 {d}일","{G} {y1}/{M1}/{d1}","{G} (일: {d}일)","{G} (월: {M}월)","{G} {M}. {d}.","{G} (월: {M})","{G} {M} {d}일","{G} {y}년","{G} {y}년 (일: {d}일)","{G} {y}/{M}","{G} {y}/{M}/{d}","{G} {y}년 {M}","{G} {y}년 {M} {d}일","{E}","{d}일 {E}","{M}월 {E}","{M}. {d}. {E}","{M} {E}","{M} {d}일 {E}","{E} (월: {M})","{y}년 {E}","{y}년 {d}일 {E}","{y}. {M}. {E}","{y}. {M}. {d}. {E}","{y}. {M1}. {E}","{y}년 {M} {E}","{y}년 {M} {d}일 {E}","{G} {E}","{G} {d}일 {E}","{G} {M}. {d}. {E}","{G} {M} {d}일 {E}","{G} {y}년 {E}","{G} {y}년 (일: {d}일 {E})","{G} {y}/{M} {E}","{G} {y}/{M}/{d} ({E})","{G} {y}년 {M} {E}","{G} {y}년 {M} {d}일 {E}","{G} {y}년 {d}일 {E}","{d}일 ({E})","{M}. {d}. ({E})","{E} (월: {M}월)","{M} {d}일 ({E})","{y}년 {d}일 ({E})","{y}. {M}. {d}. ({E})","{y}년 {M} {d}일 ({E})","{G} {d}일 ({E})","{G} {M}. {d}. ({E})","{G} {M} {d}일 ({E})","{G} {y}년 (일: {d}일 ({E}))","{G} {y}년 {M} {d}일 ({E})","{G} {y}년 {d}일 ({E})"],"011233233455455455677899:99;<<;<<;<<6=7899:99;<<;<<;<<>??@AA@AABCCBCCBCCDEEFGGFGGHIIHIIHIIDEEFGGFGGHIIHIIHIIJKKLMMLMMNOONOOPOOQRRSTTUTTVWWVWWVWWQRRSTTUTTVWWVWWVWWXYY@ZZ@ZZB[[B[[X[[]^^_aa_aabccbccbcc]d^_aa_aabccbccbccJeeLffgffPhhNhhPhhQiiSjjUjjVWWVkkVkkQiiSjjUjjVWWVkkVkkXll@mmXmmXnnBnnXnn]oo_aa_aabppbppbpp]qo_aa_aabppbppbppJeeLffLffNhhNhhNhhQiiSjjUjjVWWVkkVkkQiiSjjUjjVWWVkkVkkXll@mm@mmBnnBnnBnn]oo_aa_aabppbppbpp]oo_aa_aabppbppbpp"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{a} {h}시","{a} {h}시 {z}","{a} {h}시 (초: {s})","{a} {h}시 {z} (초: {s})","{a} {h}시 (초: {s1})","{a} {h}시 {z} (초: {s1})","{a} {h}:{m2}","{a} {h}:{m2} {z}","{a} {h}:{m2}:{s2}","{a} {h}시 {m}분 {s}초 {z}","{a} {h}:{m2}:{s}","{a} {h}시 {m}분 {s1}초 {z}","{a} {h}:{m}","{a} {h}:{m} {z}","{a} {h}:{m}:{s2}","{a} {h}시 {m1}분 {s}초 {z}","{a} {h}:{m}:{s}","{a} {h}시 {m1}분 {s1}초 {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h}시","{h}시 {z}","{h}시 (초: {s})","{h}시 {z} (초: {s})","{h}시 (초: {s1})","{h}시 {z} (초: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h}시 {m}분 {s}초","{h}시 {m}분 {s}초 {z}","{h}시 {m}분 {s1}초","{h}시 {m}분 {s1}초 {z}","{h2}:{m}","{h2}:{m} {z}","{h}시 {m1}분 {s}초","{h}시 {m1}분 {s}초 {z}","{h}:{m}:{s}","{h}시 {m1}분 {s1}초 {z}","{s} (시: {h}시)","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s}","{h}:{m}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABSDEFTUIJVLWXYPQR"],"wt":{"l":[["{1} {0}","({1}) {0}","{1} {0}","{1} {0}",{"18":"({E}) {a} {h}","36":"({E}) {a} {h}"}],["{1} {0}","({1}) {0}","{1} {0}","{1} {0}",{"26":"({E}) {h2}:{m2}:{s2}","28":"({E}) {h2}:{m2}:{s}","32":"({E}) {h2}:{m}:{s2}","34":"({E}) {h2}:{m}:{s}","44":"({E}) {h}:{m2}:{s2}"}]],"s":0,"n":0},"g":{"f":"{1} {0}","l":"{1} {0}","m":"{1} {0}","s":"{1} {0}"},"st":{"d":["{y1}년 {Ml} {d1}일 {El}","{y1}년 {Ml} {d1}일","{y1}. {M1}. {d1}.","{y2}. {M1}. {d1}."],"t12":["{a} {h1}시 {m1}분 {s1}초 {zl}","{a} {h1}시 {m1}분 {s1}초 {zs}","{a} {h1}:{m2}:{s2}","{a} {h1}:{m2}"],"t23":["{h1}시 {m1}분 {s1}초 {zl}","{h1}시 {m1}분 {s1}초 {zs}","{h1}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} {0}","{1} {0}","{1} {0}","{1} {0}"]},"hc":"h12","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","협정 세계시"],"mz":{"5":["","","동유럽 표준시","동유럽 하계 표준시","동유럽 시간"],"6":["","","중부유럽 표준시","중부유럽 하계 표준시","중부유럽 시간"],"12":["","","브라질리아 표준시","","브라질리아 표준시"],"18":["","","미 산악 표준시","미 산지 하계 표준시","미 산지 시간"],"22":["","","미 중부 표준시","미 중부 하계 표준시","미 중부 시간"],"25":["","","미 동부 표준시","미 동부 하계 표준시","미 동부 시간"],"35":["","","미 태평양 표준시","미 태평양 하계 표준시","미 태평양 시간"],"45":["","","호주 동부 표준시","호주 동부 하계 표준시","호주 동부 시간"],"61":["","","인도 표준시","","인도 표준시"],"77":["","","중국 표준시","","중국 표준시"],"82":["","","한국 표준시","","한국 표준시"],"90":["","","일본 표준시","","일본 표준시"],"111":["","","모스크바 표준시","","모스크바 표준시"],"112":["","","그리니치 표준시","영국 하계 표준시","영국 시간"]},"zg":{},"rs":" ~ ","fs":".","num":{"dec":".","grp":",","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"NaN","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["#%","-#%"],"c":["¤#","-¤#"],"ca":["¤#","(¤#)"],"cc":["¤ #","-¤ #"],"cn":["# ¤","-# ¤"]},"cs":[[3,"#C","천"],[4,"#C","만"],[4,"#C","만"],[4,"#C","만"],[4,"#C","만"],[8,"#C","억"],[8,"#C","억"],[8,"#C","억"],[8,"#C","억"],[12,"#C","조"],[12,"#C","조"],[12,"#C","조"]],"cl":[[3,"#C","천"],[4,"#C","만"],[4,"#C","만"],[4,"#C","만"],[4,"#C","만"],[8,"#C","억"],[8,"#C","억"],[8,"#C","억"],[8,"#C","억"],[12,"#C","조"],[12,"#C","조"],[12,"#C","조"]],"cur":{"USD":["미국 달러","US$"],"EUR":["유로"],"GBP":["영국 파운드"],"JPY":["일본 엔화","JP¥"],"CNY":["중국 위안화"],"INR":["인도 루피"],"CAD":["캐나다 달러"],"AUD":["호주 달러","AU$"],"CHF":["스위스 프랑"],"SEK":["스웨덴 크로나"],"PLN":["폴란드 즈워티"],"RUB":["러시아 루블"],"BRL":["브라질 레알"],"MXN":["멕시코 페소"],"KRW":["대한민국 원"],"TWD":[0,0,"NT$"],"PHP":[0,"PHP"]},"unit":{"byte":{"l":"{0}바이트","s":"{0}byte","n":0},"kilobyte":{"l":"{0}킬로바이트","s":"{0}kB","n":0},"megabyte":{"l":"{0}메가바이트","s":"{0}MB","n":0},"gigabyte":{"l":"{0}기가바이트","s":"{0}GB","n":0},"terabyte":{"l":"{0}테라바이트","s":"{0}TB","n":0},"percent":{"l":"{0}%","s":"{0}%","n":0},"millisecond":{"l":"{0}밀리초","s":"{0}ms","n":0},"second":{"l":"{0}초","s":"{0}초","n":0},"minute":{"l":"{0}분","s":"{0}분","n":0},"hour":{"l":"{0}시간","s":"{0}시간","n":0},"day":{"l":"{0}일","s":"{0}일","n":0},"week":{"l":"{0}주","s":"{0}주","n":0},"month":{"l":"{0}개월","s":"{0}개월","n":0},"year":{"l":"{0}년","s":"{0}년","n":0},"kilometer":{"l":"{0}킬로미터","s":"{0}km","n":0},"kilometer-per-hour":{"l":"시속 {0}킬로미터","s":"{0}km/h","n":0}},"rng":"{0}~{1}","approx":"~","rngS":" ~ "},"rel":{"l":{"year":["{0}년 후","{0}년 전",{"0":"올해","1":"내년","-1":"작년"}],"quarter":["{0}분기 후","{0}분기 전",{"0":"이번 분기","1":"다음 분기","-1":"지난 분기"}],"month":["{0}개월 후","{0}개월 전",{"0":"이번 달","1":"다음 달","-1":"지난달"}],"week":["{0}주 후","{0}주 전",{"0":"이번 주","1":"다음 주","-1":"지난주"}],"day":["{0}일 후","{0}일 전",{"0":"오늘","1":"내일","2":"모레","-2":"그저께","-1":"어제"}],"hour":["{0}시간 후","{0}시간 전",{"0":"현재 시간"}],"minute":["{0}분 후","{0}분 전",{"0":"현재 분"}],"second":["{0}초 후","{0}초 전",{"0":"지금"}]},"s":{"year":["{0}년 후","{0}년 전",{"0":"올해","1":"내년","-1":"작년"}],"quarter":["{0}분기 후","{0}분기 전",{"0":"이번 분기","1":"다음 분기","-1":"지난 분기"}],"month":["{0}개월 후","{0}개월 전",{"0":"이번 달","1":"다음 달","-1":"지난달"}],"week":["{0}주 후","{0}주 전",{"0":"이번 주","1":"다음 주","-1":"지난주"}],"day":["{0}일 후","{0}일 전",{"0":"오늘","1":"내일","2":"모레","-2":"그저께","-1":"어제"}],"hour":["{0}시간 후","{0}시간 전",{"0":"현재 시간"}],"minute":["{0}분 후","{0}분 전",{"0":"현재 분"}],"second":["{0}초 후","{0}초 전",{"0":"지금"}]},"n":{"year":0,"quarter":0,"month":0,"week":0,"day":0,"hour":0,"minute":0,"second":0}},"list":{"cl":["{0} 및 {1}","{0}, {1}","{0}, {1}","{0} 및 {1}"],"cs":["{0} 및 {1}","{0}, {1}","{0}, {1}","{0} 및 {1}"],"cn":["{0} 및 {1}","{0}, {1}","{0}, {1}","{0} 및 {1}"],"dl":["{0} 또는 {1}","{0}, {1}","{0}, {1}","{0} 또는 {1}"],"ds":["{0} 또는 {1}","{0}, {1}","{0}, {1}","{0} 또는 {1}"],"dn":["{0} 또는 {1}","{0}, {1}","{0}, {1}","{0} 또는 {1}"],"ul":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"us":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"fld":{"l":["연호","년","분기","월","주","요일","일","오전/오후","시","분","초","시간대"],"s":["연호","년","분기","월","주","요일","일","오전/오후","시","분","초","시간대"],"n":0},"lp":["{0}({1})",", "]}}`,
    "ru": `{"n":{"Ml":["января","февраля","марта","апреля","мая","июня","июля","августа","сентября","октября","ноября","декабря"],"Ll":["январь","февраль","март","апрель","май","июнь","июль","август","сентябрь","октябрь","ноябрь","декабрь"],"El":["воскресенье","понедельник","вторник","среда","четверг","пятница","суббота"],"cl":0,"Gl":["до Рождества Христова","от Рождества Христова"],"Bl":[0,"ночи",4,"утра",12,"полдень",13,"дня",18,"вечера",22,"ночи"],"Bl0":["ночи","полдень"],"Ms":["янв.","февр.","мар.","апр.","мая","июн.","июл.","авг.","сент.","окт.","нояб.","дек."],"Ls":["янв.","февр.","март","апр.","май","июнь","июль","авг.","сент.","окт.","нояб.","дек."],"Es":["вс","пн","вт","ср","чт","пт","сб"],"cs":0,"Gs":["до н. э.","н. э."],"Bs":[0,"ночи",4,"утра",12,"полд.",13,"дня",18,"вечера",22,"ночи"],"Bs0":["ночи","полд."],"Mn":["Я","Ф","М","А","М","И","И","А","С","О","Н","Д"],"Ln":0,"En":["В","П","В","С","Ч","П","С"],"cn":0,"Gn":["до н.э.","н.э."],"Bn":[0,"ночи",4,"утра",12,"полд.",13,"дня",18,"веч.",22,"ночи"],"Bn0":["ночи","полд."],"a":["AM","PM"]},"dp":[["","{d}","{M}","{d2}.{M2}","{d}.{M}","{L}","{d} {M}","{y}","{y} (день: {d})","{M2}.{y}","{d2}.{M2}.{y}","{d}.{M}.{y}","{M}.{y}","{L} {y} г.","{d} {M} {y} г.","{M} {y} г.","{y} {d}","{d2}.{M2}.{y1} {G}","{G} (день: {d})","{G} (месяц: {M})","{G} {d2}.{M2}","{G} {d}.{M}","{G} (месяц: {L})","{G} {d} {M}","{y} г. {G}","{y} г. {G} (день: {d})","{M2}.{y} {G}","{d2}.{M2}.{y} {G}","{d}.{M2}.{y} {G}","{M}.{y} {G}","{d2}.{M}.{y} {G}","{d}.{M}.{y} {G}","{L} {y} г. {G}","{d} {M} {y} г. {G}","{M} {y} г. {G}","{E}","{E}, {d}","{M} {E}","{E}, {d2}.{M2}","{E}, {d}.{M2}","{E}, {d2}.{M}","{E}, {d}.{M}","{L} {E}","{E}, {d} {M}","{E} (месяц: {M})","{y} {E}","{y} {E}, {d}","{M2}.{y} {E}","{E}, {d2}.{M2}.{y}","{E}, {d}.{M2}.{y}","{M}.{y} {E}","{E}, {d2}.{M}.{y}","{E}, {d}.{M}.{y}","{L} {y} г. {E}","{E}, {d} {M} {y} г.","{M} {y} г. {E}","{G} {E}","{G} {E}, {d}","{G} {E}, {d2}.{M2}","{G} {E}, {d}.{M2}","{G} {E}, {d2}.{M}","{G} {E}, {d}.{M}","{G} {E}, {d} {M}","{y} г. {G} {E}","{y} г. {G} (день: {E}, {d})","{M2}.{y} {G} {E}","{E}, {d2}.{M2}.{y} {G}","{E}, {d}.{M2}.{y} {G}","{M}.{y} {G} {E}","{E}, {d2}.{M}.{y} {G}","{E}, {d}.{M}.{y} {G}","{L} {y} г. {G} {E}","{E}, {d} {M} {y} г. {G}","{M} {y} г. {G} {E}","{E} (месяц: {L})","{y} г. {G} {E}, {d}"],"0112342445665662667889:;<;;=>>=>>?>>7@89:;<;;=>>=>>?>>ABBCDECEEFGGFGGCGGHIIJKLMNOPQQPQQRQQHIIJKLMNOPQQPQQRQQSTTUVWUXYZ[[Z[[][[^__abcdefghhghhihh^__abcdefghhghhihhjkkClmCnoFppFppjppqrrstuvwxyzzyzz{zzqrrstuvwxyzzyzz{zzSTTUVW]XY|[[Z[[][[^__abcdefghhghhihh^__abcdefghhghhihhjkkClmjnojppFppjppqrrstuvwxyzzyzz{zzq}rstuvwxyzzyzz{zzSTTUVWUXYZ[[Z[[U[[^__abcdefghhghhihh^__abcdefghhghhihhjkkClmCnoFppFppCppqrrstuvwxyzzyzz{zzqrrstuvwxyzzyzz{zz"],"tp12":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h} {a}","{h} {a}, {z}","{h} {a} (секунда: {s})","{h} {a}, {z} (секунда: {s})","{h} {a} (секунда: {s1})","{h} {a}, {z} (секунда: {s1})","{h}:{m2} {a}","{h}:{m2} {a} {z}","{h}:{m2}:{s2} {a}","{h}:{m2}:{s2} {a} {z}","{h}:{m2}:{s} {a}","{h}:{m2}:{s} {a} {z}","{h}:{m} {a}","{h}:{m} {a} {z}","{h}:{m}:{s2} {a}","{h}:{m}:{s2} {a} {z}","{h}:{m}:{s} {a}","{h}:{m}:{s} {a} {z}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRABCDEFGHIJKLMNOPQR"],"tp23":[["","{s}","{s} {z}","{s1}","{s1} {z}","{m}","{m} {z}","{m2}:{s2}","{m2}:{s2} {z}","{m2}:{s}","{m2}:{s} {z}","{m1}","{m1} {z}","{m}:{s2}","{m}:{s2} {z}","{m}:{s}","{m}:{s} {z}","{h}","{h2} {z}","{h} (секунда: {s})","{h2} {z} (секунда: {s})","{h} (секунда: {s1})","{h2} {z} (секунда: {s1})","{h2}:{m2}","{h2}:{m2} {z}","{h2}:{m2}:{s2}","{h}:{m2}:{s2} {z}","{h2}:{m2}:{s}","{h}:{m2}:{s} {z}","{h}:{m}","{h2}:{m} {z}","{h2}:{m}:{s2}","{h}:{m}:{s2} {z}","{h}:{m}:{s}","{h}:{m}:{s} {z}","{h} {z}","{s} (час: {h})","{h} {z} (секунда: {s})","{h} {z} (секунда: {s1})","{h}:{m2}","{h}:{m2} {z}","{h}:{m2}:{s2}","{h}:{m2}:{s}","{h}:{m} {z}","{h}:{m}:{s2}"],"00123456789:;<=>?@ABCDEFGHIJKLMNOPQRASTUEVWXYJZLM[]PQR"],"wt":{"l":[["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}"],["{1}, {0}","{1} {0}","{1}, {0}","{1}, {0}",{"30":"{E} {h2}:{m}","34":"{E} {h2}:{m}:{s}"}]],"s":0,"n":0},"g":{"f":"{1} в {0}","l":"{1} в {0}","m":"{1}, {0}","s":"{1}, {0}"},"st":{"d":["{El}, {d1} {Ml} {y1} г.","{d1} {Ml} {y1} г.","{d1} {Ms} {y1} г.","{d2}.{M2}.{y1}"],"t12":["{h2}:{m2}:{s2} {a} {zl}","{h2}:{m2}:{s2} {a} {zs}","{h2}:{m2}:{s2} {a}","{h2}:{m2} {a}"],"t23":["{h2}:{m2}:{s2} {zl}","{h2}:{m2}:{s2} {zs}","{h2}:{m2}:{s2}","{h2}:{m2}"],"g":["{1} в {0}","{1} в {0}","{1}, {0}","{1}, {0}"]},"hc":"h23","hc12":"h12","gmt":["GMT{0}","GMT+0","+HH:mm","-HH:mm","UTC","Всемирное координированное время"],"mz":{"5":["","","Восточная Европа, стандартное время","Восточная Европа, летнее время","Восточная Европа"],"6":["","","Центральная Европа, стандартное время","Центральная Европа, летнее время","Центральная Европа"],"12":["","","Бразилия, стандартное время","","Бразилия, стандартное время"],"18":["","","Стандартное горное время (Северная Америка)","Летнее горное время (Северная Америка)","Горное время (Северная Америка)"],"22":["","","Центральная Америка, стандартное время","Центральная Америка, летнее время","Центральная Америка"],"25":["","","Восточная Америка, стандартное время","Восточная Америка, летнее время","Восточная Америка"],"35":["","","Тихоокеанское стандартное время","Тихоокеанское летнее время","Тихоокеанское время"],"45":["","","Восточная Австралия, стандартное время","Восточная Австралия, летнее время","Восточная Австралия"],"61":["","","Индия","","Индия"],"77":["","","Китай, стандартное время","","Китай, стандартное время"],"82":["","","Корея, стандартное время","","Корея, стандартное время"],"90":["","","Япония, стандартное время","","Япония, стандартное время"],"111":["","","Москва, стандартное время","","Москва, стандартное время"],"112":["","","Среднее время по Гринвичу","Великобритания, летнее время","Великобритания"]},"zg":{},"rs":" – ","fs":",","num":{"dec":",","grp":" ","min":"-","plus":"+","pct":"%","g1":3,"g2":3,"mg":1,"nan":"не число","inf":"∞","exp":"E","pat":{"d":["#","-#"],"p":["# %","-# %"],"c":["# ¤","-# ¤"],"ca":["# ¤","-# ¤"],"cc":["# ¤","-# ¤"],"cn":["# ¤","-# ¤"]},"cs":[[3,"# C","тыс."],[3,"# C","тыс."],[3,"# C","тыс."],[6,"# C","млн"],[6,"# C","млн"],[6,"# C","млн"],[9,"# C","млрд"],[9,"# C","млрд"],[9,"# C","млрд"],[12,"# C","трлн"],[12,"# C","трлн"],[12,"# C","трлн"]],"cl":[[[3,"# C","тысяча"],[3,"# C","тысячи"],[3,"# C","тысяч"],[3,"# C","тысячи"]],[3,"# C","тысяч"],[3,"# C","тысяч"],[[6,"# C","миллион"],[6,"# C","миллиона"],[6,"# C","миллионов"],[6,"# C","миллиона"]],[6,"# C","миллионов"],[6,"# C","миллионов"],[[9,"# C","миллиард"],[9,"# C","миллиарда"],[9,"# C","миллиардов"],[9,"# C","миллиарда"]],[9,"# C","миллиардов"],[9,"# C","миллиардов"],[[12,"# C","триллион"],[12,"# C","триллиона"],[12,"# C","триллионов"],[12,"# C","триллиона"]],[12,"# C","триллионов"],[12,"# C","триллионов"]],"cur":{"USD":[["доллар США","доллара США","долларов США","доллара США"]],"EUR":["евро"],"GBP":[["британский фунт стерлингов","британских фунта стерлингов","британских фунтов стерлингов","британского фунта стерлингов"]],"JPY":[["японская иена","японские иены","японских иен","японской иены"]],"CNY":[["китайский юань","китайских юаня","китайских юаней","китайского юаня"]],"INR":[["индийская рупия","индийские рупии","индийских рупий","индийской рупии"]],"CAD":[["канадский доллар","канадских доллара","канадских долларов","канадского доллара"]],"AUD":[["австралийский доллар","австралийских доллара","австралийских долларов","австралийского доллара"]],"CHF":[["швейцарский франк","швейцарских франка","швейцарских франков","швейцарского франка"]],"SEK":[["шведская крона","шведские кроны","шведских крон","шведской кроны"]],"PLN":[["польский злотый","польских злотых","польских злотых","польского злотого"]],"RUB":[["российский рубль","российских рубля","российских рублей","российского рубля"],"₽"],"BRL":[["бразильский реал","бразильских реала","бразильских реалов","бразильского реала"]],"MXN":[["мексиканский песо","мексиканских песо","мексиканских песо","мексиканского песо"]],"KRW":[["южнокорейская вона","южнокорейские воны","южнокорейских вон","южнокорейской воны"]],"TWD":[0,0,"NT$"],"THB":[0,"฿"],"PHP":[0,"PHP"],"UAH":[0,"₴"]},"unit":{"byte":{"l":["{0} байт","{0} байта","{0} байт","{0} байта"],"s":"{0} Б","n":0},"kilobyte":{"l":["{0} килобайт","{0} килобайта","{0} килобайт","{0} килобайта"],"s":"{0} кБ","n":0},"megabyte":{"l":["{0} мегабайт","{0} мегабайта","{0} мегабайт","{0} мегабайта"],"s":"{0} МБ","n":0},"gigabyte":{"l":["{0} гигабайт","{0} гигабайта","{0} гигабайт","{0} гигабайта"],"s":"{0} ГБ","n":0},"terabyte":{"l":["{0} терабайт","{0} терабайта","{0} терабайт","{0} терабайта"],"s":"{0} ТБ","n":0},"percent":{"l":["{0} процент","{0} процента","{0} процентов","{0} процента"],"s":"{0} %","n":0},"millisecond":{"l":["{0} миллисекунда","{0} миллисекунды","{0} миллисекунд","{0} миллисекунды"],"s":"{0} мс","n":0},"second":{"l":["{0} секунда","{0} секунды","{0} секунд","{0} секунды"],"s":"{0} с","n":0},"minute":{"l":["{0} минута","{0} минуты","{0} минут","{0} минуты"],"s":"{0} мин","n":0},"hour":{"l":["{0} час","{0} часа","{0} часов","{0} часа"],"s":"{0} ч","n":0},"day":{"l":["{0} день","{0} дня","{0} дней","{0} дня"],"s":"{0} дн.","n":"{0} д."},"week":{"l":["{0} неделя","{0} недели","{0} недель","{0} недели"],"s":"{0} нед.","n":"{0} н."},"month":{"l":["{0} месяц","{0} месяца","{0} месяцев","{0} месяца"],"s":"{0} мес.","n":"{0} м."},"year":{"l":["{0} год","{0} года","{0} лет","{0} года"],"s":["{0} г.","{0} г.","{0} л.","{0} г."],"n":0},"kilometer":{"l":["{0} километр","{0} километра","{0} километров","{0} километра"],"s":"{0} км","n":0},"kilometer-per-hour":{"l":["{0} километр в час","{0} километра в час","{0} километров в час","{0} километра в час"],"s":"{0} км/ч","n":0}},"rng":"{0}–{1}","approx":"≈","rngS":""},"rel":{"l":{"year":[["через {0} год","через {0} года","через {0} лет","через {0} года"],["{0} год назад","{0} года назад","{0} лет назад","{0} года назад"],{"0":"в этом году","1":"в следующем году","-1":"в прошлом году"}],"quarter":[["через {0} квартал","через {0} квартала","через {0} кварталов","через {0} квартала"],["{0} квартал назад","{0} квартала назад","{0} кварталов назад","{0} квартала назад"],{"0":"в текущем квартале","1":"в следующем квартале","-1":"в прошлом квартале"}],"month":[["через {0} месяц","через {0} месяца","через {0} месяцев","через {0} месяца"],["{0} месяц назад","{0} месяца назад","{0} месяцев назад","{0} месяца назад"],{"0":"в этом месяце","1":"в следующем месяце","-1":"в прошлом месяце"}],"week":[["через {0} неделю","через {0} недели","через {0} недель","через {0} недели"],["{0} неделю назад","{0} недели назад","{0} недель назад","{0} недели назад"],{"0":"на этой неделе","1":"на следующей неделе","-1":"на прошлой неделе"}],"day":[["через {0} день","через {0} дня","через {0} дней","через {0} дня"],["{0} день назад","{0} дня назад","{0} дней назад","{0} дня назад"],{"0":"сегодня","1":"завтра","2":"послезавтра","-2":"позавчера","-1":"вчера"}],"hour":[["через {0} час","через {0} часа","через {0} часов","через {0} часа"],["{0} час назад","{0} часа назад","{0} часов назад","{0} часа назад"],{"0":"в этот час"}],"minute":[["через {0} минуту","через {0} минуты","через {0} минут","через {0} минуты"],["{0} минуту назад","{0} минуты назад","{0} минут назад","{0} минуты назад"],{"0":"в эту минуту"}],"second":[["через {0} секунду","через {0} секунды","через {0} секунд","через {0} секунды"],["{0} секунду назад","{0} секунды назад","{0} секунд назад","{0} секунды назад"],{"0":"сейчас"}]},"s":{"year":[["через {0} г.","через {0} г.","через {0} л.","через {0} г."],["{0} г. назад","{0} г. назад","{0} л. назад","{0} г. назад"],{"0":"в этом г.","1":"в след. г.","-1":"в прошлом г."}],"quarter":["через {0} кв.","{0} кв. назад",{"0":"текущий кв.","1":"следующий кв.","-1":"последний кв."}],"month":["через {0} мес.","{0} мес. назад",{"0":"в этом мес.","1":"в следующем мес.","-1":"в прошлом мес."}],"week":["через {0} нед.","{0} нед. назад",{"0":"на этой нед.","1":"на следующей нед.","-1":"на прошлой нед."}],"day":["через {0} дн.","{0} дн. назад",{"0":"сегодня","1":"завтра","2":"послезавтра","-2":"позавчера","-1":"вчера"}],"hour":["через {0} ч","{0} ч назад",{"0":"в этот час"}],"minute":["через {0} мин.","{0} мин. назад",{"0":"в эту минуту"}],"second":["через {0} сек.","{0} сек. назад",{"0":"сейчас"}]},"n":{"year":[["+{0} г.","+{0} г.","+{0} л.","+{0} г."],["-{0} г.","-{0} г.","-{0} л.","-{0} г."],{"0":"в эт. г.","1":"в сл. г.","-1":"в пр. г."}],"quarter":["+{0} кв.","-{0} кв.",{"0":"тек. кв.","1":"след. кв.","-1":"посл. кв."}],"month":["+{0} мес.","-{0} мес.",{"0":"в эт. мес.","1":"в след. мес.","-1":"в пр. мес."}],"week":["+{0} нед.","-{0} нед.",{"0":"на эт. нед.","1":"на след. нед.","-1":"на пр. нед."}],"day":["+{0} дн.","-{0} дн.",{"0":"сегодня","1":"завтра","2":"послезавтра","-2":"позавчера","-1":"вчера"}],"hour":["+{0} ч","-{0} ч",{"0":"в этот час"}],"minute":["+{0} мин","-{0} мин",{"0":"в эту минуту"}],"second":["+{0} с","-{0} с",{"0":"сейчас"}]}},"list":{"cl":["{0} и {1}","{0}, {1}","{0}, {1}","{0} и {1}"],"cs":["{0} и {1}","{0}, {1}","{0}, {1}","{0} и {1}"],"cn":["{0}, {1}","{0}, {1}","{0}, {1}","{0}, {1}"],"dl":["{0} или {1}","{0}, {1}","{0}, {1}","{0} или {1}"],"ds":["{0} или {1}","{0}, {1}","{0}, {1}","{0} или {1}"],"dn":["{0} или {1}","{0}, {1}","{0}, {1}","{0} или {1}"],"ul":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"us":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"],"un":["{0} {1}","{0} {1}","{0} {1}","{0} {1}"]},"dn":{"fld":{"l":["эра","год","квартал","месяц","неделя","день недели","день","AM/PM","час","минута","секунда","часовой пояс"],"s":["эра","г.","кв.","мес.","нед.","дн. недели","дн.","AM/PM","ч","мин.","сек.","час. пояс"],"n":["эра","г.","кв.","мес.","нед.","дн. нед.","дн.","AM/PM","ч","мин","с","час. пояс"]},"lp":["{0} ({1})",", "]}}`,
  };
  const LOCALE_PARENT = {"en-GB":"en","en-CA":"en","en-AU":"en","en-IN":"en"};
  // ---- end of generated data ----

  // ---------- locale tags ----------
  const LANG_ALIAS = { iw: 'he', in: 'id', ji: 'yi', jw: 'jv', mo: 'ro', tl: 'fil', sh: 'sr-Latn', aar: 'aa', eng: 'en', deu: 'de', ger: 'de', fra: 'fr', fre: 'fr', spa: 'es', jpn: 'ja', zho: 'zh', chi: 'zh', rus: 'ru', cmn: 'zh', swc: 'sw', prs: 'fa-AF', tw: 'ak' };
  const REGION_ALIAS = { BU: 'MM', DD: 'DE', FX: 'FR', TP: 'TL', YD: 'YE', ZR: 'CD' };
  const ALNUM = /^[a-z0-9]+$/;

  // parse a BCP 47 tag into canonical pieces, or null when malformed
  function parseTag(tag) {
    if (typeof tag !== 'string' || !tag || tag.length > 255) return null;
    const s = tag.toLowerCase().split('-');
    let i = 0;
    const r = { lang: '', script: '', region: '', variants: [], ext: [], priv: '' };
    const lang = s[i];
    if (!/^[a-z]{2,3}$|^[a-z]{5,8}$/.test(lang) || lang === 'root') return null;
    r.lang = lang;
    i++;
    if (s[i] && /^[a-z]{4}$/.test(s[i])) r.script = s[i][0].toUpperCase() + s[i++].slice(1);
    if (s[i] && /^([a-z]{2}|\d{3})$/.test(s[i])) r.region = s[i++].toUpperCase();
    while (s[i] && /^([a-z0-9]{5,8}|\d[a-z0-9]{3})$/.test(s[i])) {
      if (r.variants.includes(s[i])) return null;
      r.variants.push(s[i++]);
    }
    const seen = new Set();
    while (i < s.length) {
      const single = s[i];
      if (single.length !== 1 || !ALNUM.test(single)) return null;
      if (single === 'x') {
        const rest = s.slice(i + 1);
        if (!rest.length || !rest.every((x) => /^[a-z0-9]{1,8}$/.test(x))) return null;
        r.priv = 'x-' + rest.join('-');
        break;
      }
      if (seen.has(single)) return null;
      seen.add(single);
      const parts = [];
      i++;
      while (i < s.length && s[i].length > 1) {
        if (!/^[a-z0-9]{2,8}$/.test(s[i])) return null;
        parts.push(s[i++]);
      }
      if (!parts.length) return null;
      r.ext.push([single, parts]);
    }
    return r;
  }

  // -u- extension: [attributes, [[key, value], ...]]
  function parseU(parts) {
    const attrs = [], kws = [];
    let j = 0;
    while (j < parts.length && parts[j].length >= 3) attrs.push(parts[j++]);
    while (j < parts.length) {
      const key = parts[j++];
      if (!/^[a-z0-9][a-z]$/.test(key)) return null;
      const val = [];
      while (j < parts.length && parts[j].length >= 3) val.push(parts[j++]);
      if (!kws.some((k) => k[0] === key)) kws.push([key, val.join('-')]);
    }
    return [attrs, kws];
  }

  function tagToString(r) {
    let s = r.lang;
    if (r.script) s += '-' + r.script;
    if (r.region) s += '-' + r.region;
    if (r.variants.length) s += '-' + r.variants.join('-');
    for (const [k, parts] of r.ext) s += '-' + k + '-' + parts.join('-');
    if (r.priv) s += '-' + r.priv;
    return s;
  }

  const canonCache = new Map();
  function canonTag(tag) {
    let c = canonCache.get(tag);
    if (c !== undefined) return c;
    const r = parseTag(tag);
    if (!r) throw rangeError('Incorrect locale information provided');
    const alias = LANG_ALIAS[r.lang];
    if (alias) {
      const a = alias.split('-');
      r.lang = a[0];
      if (a[1] && !r.script) r.script = a[1];
    }
    if (REGION_ALIAS[r.region]) r.region = REGION_ALIAS[r.region];
    r.variants.sort();
    for (const e of r.ext) {
      if (e[0] === 'u') {
        const u = parseU(e[1]);
        if (!u) throw rangeError('Incorrect locale information provided');
        u[0].sort();
        u[1].sort((a, b) => (a[0] < b[0] ? -1 : 1));
        e[1] = [...u[0]];
        for (const [k, v] of u[1]) { e[1].push(k); if (v && v !== 'true') e[1].push(...v.split('-')); }
      }
    }
    r.ext.sort((a, b) => (a[0] < b[0] ? -1 : 1));
    c = tagToString(r);
    if (canonCache.size > 500) canonCache.clear();
    canonCache.set(tag, c);
    return c;
  }

  // CanonicalizeLocaleList
  function canonList(locales) {
    if (locales === undefined) return [];
    if (typeof locales === 'string' || locales instanceof IntlLocale) return [canonTag(locales instanceof IntlLocale ? slot(locales, 'Locale').tag : locales)];
    if (locales === null) throw typeError('Cannot convert undefined or null to object');
    const O = Object(locales), out = [];
    const len = Math.min(Number(O.length) || 0, 2 ** 32);
    for (let k = 0; k < len; k++) {
      if (!(k in O)) continue;
      const v = O[k];
      if (typeof v !== 'string' && (typeof v !== 'object' || v === null)) throw typeError('Language ID should be string or object.');
      const t = canonTag(v instanceof IntlLocale ? slot(v, 'Locale').tag : String(v));
      if (!out.includes(t)) out.push(t);
    }
    return out;
  }

  // ---------- locale data ----------
  const DATA_KEYS = new Set(Object.keys(LOCALE_DATA));
  const EN_US_REGIONS = new Set(['US', 'AS', 'GU', 'MP', 'PR', 'UM', 'VI']);
  // the data locale for a tag without extensions, or '' when unsupported
  function dataKey(base) {
    const r = parseTag(base);
    if (!r) return '';
    const lang = r.lang;
    if (lang === 'en') {
      if (r.region && DATA_KEYS.has('en-' + r.region)) return 'en-' + r.region;
      if (r.region && !EN_US_REGIONS.has(r.region)) return 'en-GB';
      return 'en';
    }
    if (r.region && DATA_KEYS.has(lang + '-' + r.region)) return lang + '-' + r.region;
    if (lang === 'nb' || lang === 'nn') return DATA_KEYS.has('nb') ? 'nb' : '';
    return DATA_KEYS.has(lang) ? lang : '';
  }

  // services without per-locale data (collation, segmentation) accept any language ICU knows
  const anyLang = (base) => {
    const r = parseTag(base);
    return r && (dataKey(base) || hasOwn(PLURALS, r.lang) || hasOwn(LIKELY, r.lang)) ? base : '';
  };
  const parsed = new Map();
  // merged locale data (parent first)
  function LD(key) {
    let d = parsed.get(key);
    if (d) return d;
    d = JSON.parse(LOCALE_DATA[key]);
    const par = LOCALE_PARENT[key];
    if (par) d = merge(LD(par), d, 0);
    d.key = key;
    parsed.set(key, d);
    return d;
  }
  function merge(p, d, depth) {
    const r = Object.assign({}, p);
    for (const k of Object.keys(d)) {
      const a = p[k], b = d[k];
      r[k] = depth < 2 && a && b && typeof a === 'object' && typeof b === 'object' && !Array.isArray(a) && !Array.isArray(b) ? merge(a, b, depth + 1) : b;
    }
    return r;
  }

  const stripExt = (tag) => {
    const i = tag.search(/-[a-wyz0-9]-/);
    const j = tag.indexOf('-x-');
    const cut = [i, j].filter((x) => x >= 0);
    return cut.length ? tag.slice(0, Math.min(...cut)) : tag;
  };
  function uKeywords(tag) {
    const m = /-u((?:-[a-z0-9]{2,8})+)/.exec(tag.split('-x-')[0]);
    if (!m) return {};
    const u = parseU(m[1].slice(1).split('-').filter((x) => x.length > 1 || /^[0-9a-z]$/.test(x)));
    const o = {};
    if (u) for (const [k, v] of u[1]) o[k] = v || 'true';
    return o;
  }

  let defaultLocaleCache;
  function defaultLocale() {
    if (defaultLocaleCache) return defaultLocaleCache;
    let t = 'en-US';
    try { t = stripExt(canonTag(String(H.language || 'en-US'))); } catch {}
    if (!dataKey(t)) t = 'en-US';
    return (defaultLocaleCache = t);
  }

  // ResolveLocale: { locale, key (data), ext: {k: v} } for the relevant keys
  const NUMBERING = { latn: 0x30, arab: 0x660, arabext: 0x6f0, beng: 0x9e6, deva: 0x966, fullwide: 0xff10, gujr: 0xae6, guru: 0xa66, khmr: 0x17e0, knda: 0xce6, laoo: 0xed0, mlym: 0xd66, mymr: 0x1040, orya: 0xb66, tamldec: 0xbe6, telu: 0xc66, thai: 0xe50, tibt: 0xf20 };
  const EXT_OK = {
    ca: (v) => v === 'gregory' || v === 'iso8601' ? 'gregory' : null,
    nu: (v) => (hasOwn(NUMBERING, v) ? v : null),
    hc: (v) => (['h11', 'h12', 'h23', 'h24'].includes(v) ? v : null),
    co: (v) => (v === 'emoji' || v === 'eor' ? v : null),
    kn: (v) => (v === 'true' || v === 'false' ? v : null),
    kf: (v) => (['upper', 'lower', 'false'].includes(v) ? v : null),
  };
  function resolveLocale(locales, keys, opts, avail) {
    const req = canonList(locales);
    let tag = null, base, key;
    for (const t of req) {
      const b = stripExt(t);
      const k = (avail || dataKey)(b);
      if (k) { tag = t; base = b; key = k; break; }
    }
    let isDefault = false;
    if (!tag) { base = tag = defaultLocale(); key = (avail || dataKey)(base) || dataKey(base); isDefault = true; }
    const kw = uKeywords(tag);
    const ext = {}, shown = [];
    for (const k of keys) {
      let v = hasOwn(kw, k) ? EXT_OK[k](kw[k]) : null;
      const fromTag = v !== null;
      const o = opts && opts[k];
      if (o !== undefined && o !== null) {
        if (fromTag && String(o) !== v) {
          v = o;
          ext[k] = v;
          continue;
        }
        v = o;
      }
      if (v !== null && v !== undefined) {
        ext[k] = v;
        if (fromTag) shown.push(k + (v === 'true' ? '' : '-' + v));
      }
    }
    const locale = base + (shown.length ? '-u-' + shown.join('-') : '');
    return { locale, base, key, ext, isDefault };
  }

  function supportedLocalesOf(locales, options) {
    const req = canonList(locales);
    if (options !== undefined) {
      const o = toObject(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit');
    }
    return req.filter((t) => dataKey(stripExt(t)));
  }

  // ---------- option helpers ----------
  function toObject(o) {
    if (o === null || o === undefined) throw typeError('Cannot convert undefined or null to object');
    return Object(o);
  }
  function optionsObject(o) {
    if (o === undefined) return ObjectCreate(null);
    if (typeof o === 'object' && o !== null) return o;
    if (typeof o === 'function') return o;
    throw typeError('Options must be an object');
  }
  function coerceOptions(o) { return o === undefined ? ObjectCreate(null) : toObject(o); }
  function getOpt(o, prop, type, values, fallback, ctor) {
    let v = o[prop];
    if (v === undefined) return fallback;
    if (type === 'boolean') v = Boolean(v);
    else {
      if (typeof v === 'symbol') throw typeError('Cannot convert a Symbol value to a string');
      v = String(v);
    }
    if (values && !values.includes(v)) throw rangeError('Value ' + v + ' out of range for ' + (ctor || 'Intl') + ' options property ' + prop);
    return v;
  }
  function getNum(o, prop, min, max, fallback) {
    let v = o[prop];
    if (v === undefined) return fallback;
    v = Number(v);
    if (Number.isNaN(v) || v < min || v > max) throw rangeError(prop + ' value is out of range.');
    return Math.floor(v);
  }

  // ---------- internal slots ----------
  const slots = new WeakMap();
  function slot(o, kind) {
    const s = (typeof o === 'object' || typeof o === 'function') && o !== null ? slots.get(o) : undefined;
    if (!s || s.kind !== kind) throw typeError('Method Intl.' + kind + '.prototype called on incompatible receiver ' + (o === null || o === undefined ? String(o) : typeof o === 'object' ? '#<' + ((o.constructor && o.constructor.name) || 'Object') + '>' : String(o)));
    return s;
  }
  const setSlot = (o, kind, s) => { s.kind = kind; slots.set(o, s); return s; };

  // methods/getters on prototypes, non-enumerable like the built-ins
  function methods(proto, obj) {
    for (const k of Object.keys(obj)) {
      const d = Object.getOwnPropertyDescriptor(obj, k);
      if (d.get) Object.defineProperty(proto, k, { get: d.get, configurable: true, enumerable: false });
      else {
        const f = d.value;
        // concise methods are not constructors already; keep names right
        Object.defineProperty(proto, k, { value: f, writable: true, configurable: true, enumerable: false });
      }
    }
  }
  function tag(proto, name) {
    Object.defineProperty(proto, Symbol.toStringTag, { value: name, configurable: true, writable: false, enumerable: false });
  }

  // ---------- plural rules (CLDR) ----------
  // operands of a formatted decimal string ("1.50" → n=1.5 i=1 v=2 f=50 t=5),
  // e = compact exponent
  function operands(s, e) {
    s = String(s).replace(/^-/, '');
    const dot = s.indexOf('.');
    const int = dot < 0 ? s : s.slice(0, dot), frac = dot < 0 ? '' : s.slice(dot + 1);
    const i = Number(int), ft = frac.replace(/0+$/, '');
    return { n: Number(s), i, v: frac.length, w: ft.length, f: frac ? Number(frac) : 0, t: ft ? Number(ft) : 0, e: e || 0 };
  }
  const inR = (x, a, b) => x >= a && x <= b && Number.isInteger(x);
  const manyE = (o) => (o.e === 0 && o.i !== 0 && o.i % 1000000 === 0 && o.v === 0) || !inR(o.e, 0, 5);
  const enOrd = (o) => {
    const n10 = o.n % 10, n100 = o.n % 100;
    return n10 === 1 && n100 !== 11 ? 'one' : n10 === 2 && n100 !== 12 ? 'two' : n10 === 3 && n100 !== 13 ? 'few' : 'other';
  };
  const oneIV = (o) => (o.i === 1 && o.v === 0 ? 'one' : 'other');
  const slavic = (o) => {
    if (o.v !== 0) return 'other';
    const i10 = o.i % 10, i100 = o.i % 100;
    if (i10 === 1 && i100 !== 11) return 'one';
    if (inR(i10, 2, 4) && !inR(i100, 12, 14)) return 'few';
    return 'many';
  };
  const OTHER = () => 'other';
  // [cardinal, ordinal, cardinal categories, ordinal categories]
  const PLURALS = {
    en: [oneIV, enOrd, 'one other', 'one two few other'],
    de: [oneIV, OTHER, 'one other', 'other'],
    nl: [oneIV, OTHER, 'one other', 'other'],
    sv: [oneIV, (o) => ((o.n % 10 === 1 || o.n % 10 === 2) && o.n % 100 !== 11 && o.n % 100 !== 12 ? 'one' : 'other'), 'one other', 'one other'],
    it: [(o) => (o.i === 1 && o.v === 0 ? 'one' : manyE(o) ? 'many' : 'other'), (o) => ([11, 8, 80, 800].includes(o.n) ? 'many' : 'other'), 'one many other', 'many other'],
    es: [(o) => (o.n === 1 ? 'one' : manyE(o) ? 'many' : 'other'), OTHER, 'one many other', 'other'],
    fr: [(o) => (o.i === 0 || o.i === 1 ? 'one' : manyE(o) ? 'many' : 'other'), (o) => (o.n === 1 ? 'one' : 'other'), 'one many other', 'one other'],
    pt: [(o) => (inR(o.i, 0, 1) ? 'one' : manyE(o) ? 'many' : 'other'), OTHER, 'one many other', 'other'],
    'pt-PT': [(o) => (o.i === 1 && o.v === 0 ? 'one' : manyE(o) ? 'many' : 'other'), OTHER, 'one many other', 'other'],
    ca: [(o) => (o.i === 1 && o.v === 0 ? 'one' : manyE(o) ? 'many' : 'other'), (o) => (o.n === 1 || o.n === 3 ? 'one' : o.n === 2 ? 'two' : o.n === 4 ? 'few' : 'other'), 'one many other', 'one two few other'],
    pl: [(o) => (o.i === 1 && o.v === 0 ? 'one' : o.v === 0 && inR(o.i % 10, 2, 4) && !inR(o.i % 100, 12, 14) ? 'few' : o.v === 0 ? 'many' : 'other'), OTHER, 'one few many other', 'other'],
    ru: [slavic, OTHER, 'one few many other', 'other'],
    uk: [slavic, (o) => (o.n % 10 === 3 && o.n % 100 !== 13 ? 'few' : 'other'), 'one few many other', 'few other'],
    be: [(o) => { const n10 = o.n % 10, n100 = o.n % 100; return n10 === 1 && n100 !== 11 ? 'one' : inR(n10, 2, 4) && !inR(n100, 12, 14) ? 'few' : n10 === 0 || inR(n10, 5, 9) || inR(n100, 11, 14) ? 'many' : 'other'; }, OTHER, 'one few many other', 'other'],
    cs: [(o) => (o.i === 1 && o.v === 0 ? 'one' : inR(o.i, 2, 4) && o.v === 0 ? 'few' : o.v !== 0 ? 'many' : 'other'), OTHER, 'one few many other', 'other'],
    da: [(o) => (o.n === 1 || (o.t !== 0 && (o.i === 0 || o.i === 1)) ? 'one' : 'other'), OTHER, 'one other', 'other'],
    nb: [(o) => (o.n === 1 ? 'one' : 'other'), OTHER, 'one other', 'other'],
    fi: [oneIV, OTHER, 'one other', 'other'],
    tr: [(o) => (o.n === 1 ? 'one' : 'other'), OTHER, 'one other', 'other'],
    el: [(o) => (o.n === 1 ? 'one' : 'other'), OTHER, 'one other', 'other'],
    hu: [(o) => (o.n === 1 ? 'one' : 'other'), (o) => (o.n === 1 || o.n === 5 ? 'one' : 'other'), 'one other', 'one other'],
    he: [(o) => ((o.i === 1 && o.v === 0) || (o.i === 0 && o.v !== 0) ? 'one' : o.i === 2 && o.v === 0 ? 'two' : 'other'), OTHER, 'one two other', 'other'],
    ar: [(o) => { const n100 = o.n % 100; return o.n === 0 ? 'zero' : o.n === 1 ? 'one' : o.n === 2 ? 'two' : inR(n100, 3, 10) ? 'few' : inR(n100, 11, 99) ? 'many' : 'other'; }, OTHER, 'zero one two few many other', 'other'],
    hi: [(o) => (o.i === 0 || o.n === 1 ? 'one' : 'other'), (o) => (o.n === 1 ? 'one' : o.n === 2 || o.n === 3 ? 'two' : o.n === 4 ? 'few' : o.n === 6 ? 'many' : 'other'), 'one other', 'one two few many other'],
    ro: [(o) => (o.i === 1 && o.v === 0 ? 'one' : o.v !== 0 || o.n === 0 || (o.n !== 1 && inR(o.n % 100, 1, 19)) ? 'few' : 'other'), (o) => (o.n === 1 ? 'one' : 'other'), 'one few other', 'one other'],
    ja: [OTHER, OTHER, 'other', 'other'],
  };
  for (const l of ['et', 'gl', 'ur', 'sw', 'fy', 'ia', 'io', 'ji', 'lij', 'sc', 'yi']) PLURALS[l] = [oneIV, OTHER, 'one other', 'other'];
  for (const l of ['no', 'nn', 'af', 'az', 'bg', 'ka', 'kk', 'ky', 'mn', 'sq', 'ta', 'te', 'uz', 'eu', 'ml']) PLURALS[l] = [(o) => (o.n === 1 ? 'one' : 'other'), OTHER, 'one other', 'other'];
  for (const l of ['zh', 'ko', 'id', 'ms', 'th', 'vi', 'my', 'lo', 'km', 'yue', 'jv']) PLURALS[l] = PLURALS.ja;
  PLURALS.bn = PLURALS.gu = PLURALS.kn = PLURALS.fa = PLURALS.am = PLURALS.zu = [PLURALS.hi[0], OTHER, 'one other', 'other'];
  PLURALS.sk = PLURALS.cs;
  PLURALS.sr = PLURALS.hr = PLURALS.bs = [(o) => { const i10 = o.i % 10, i100 = o.i % 100, f10 = o.f % 10, f100 = o.f % 100; return (o.v === 0 && i10 === 1 && i100 !== 11) || (f10 === 1 && f100 !== 11) ? 'one' : (o.v === 0 && inR(i10, 2, 4) && !inR(i100, 12, 14)) || (inR(f10, 2, 4) && !inR(f100, 12, 14)) ? 'few' : 'other'; }, OTHER, 'one few other', 'other'];
  PLURALS.lt = [(o) => { const n10 = o.n % 10, n100 = o.n % 100; return n10 === 1 && !inR(n100, 11, 19) ? 'one' : inR(n10, 2, 9) && !inR(n100, 11, 19) ? 'few' : o.f !== 0 ? 'many' : 'other'; }, OTHER, 'one few many other', 'other'];
  PLURALS.lv = [(o) => { const n10 = o.n % 10, n100 = o.n % 100; return n10 === 0 || inR(n100, 11, 19) || (o.v === 2 && inR(o.f % 100, 11, 19)) ? 'zero' : (n10 === 1 && n100 !== 11) || (o.v === 2 && o.f % 10 === 1 && o.f % 100 !== 11) || (o.v !== 2 && o.f % 10 === 1) ? 'one' : 'other'; }, OTHER, 'zero one other', 'other'];
  PLURALS.sl = [(o) => (o.v === 0 && o.i % 100 === 1 ? 'one' : o.v === 0 && o.i % 100 === 2 ? 'two' : (o.v === 0 && inR(o.i % 100, 3, 4)) || o.v !== 0 ? 'few' : 'other'), OTHER, 'one two few other', 'other'];
  PLURALS.ga = [(o) => (o.n === 1 ? 'one' : o.n === 2 ? 'two' : inR(o.n, 3, 6) ? 'few' : inR(o.n, 7, 10) ? 'many' : 'other'), (o) => (o.n === 1 ? 'one' : 'other'), 'one two few many other', 'one other'];
  PLURALS.cy = [(o) => (o.n === 0 ? 'zero' : o.n === 1 ? 'one' : o.n === 2 ? 'two' : o.n === 3 ? 'few' : o.n === 6 ? 'many' : 'other'), (o) => ([0, 7, 8, 9].includes(o.n) ? 'zero' : o.n === 1 ? 'one' : o.n === 2 ? 'two' : o.n === 3 || o.n === 4 ? 'few' : o.n === 5 || o.n === 6 ? 'many' : 'other'), 'zero one two few many other', 'zero one two few many other'];
  PLURALS.fil = [(o) => ((o.v === 0 && [1, 2, 3].includes(o.i)) || (o.v === 0 && ![4, 6, 9].includes(o.i % 10)) || (o.v !== 0 && ![4, 6, 9].includes(o.f % 10)) ? 'one' : 'other'), (o) => (o.n === 1 ? 'one' : 'other'), 'one other', 'one other'];
  PLURALS.is = [(o) => ((o.t === 0 && o.i % 10 === 1 && o.i % 100 !== 11) || (o.t % 10 === 1 && o.t % 100 !== 11) ? 'one' : 'other'), OTHER, 'one other', 'other'];
  PLURALS.mk = [(o) => ((o.v === 0 && o.i % 10 === 1 && o.i % 100 !== 11) || (o.f % 10 === 1 && o.f % 100 !== 11) ? 'one' : 'other'), (o) => { const i10 = o.i % 10, i100 = o.i % 100; return i10 === 1 && i100 !== 11 ? 'one' : i10 === 2 && i100 !== 12 ? 'two' : (i10 === 7 || i10 === 8) && i100 !== 17 && i100 !== 18 ? 'many' : 'other'; }, 'one other', 'one two many other'];

  function pluralKey(base) {
    const r = parseTag(base);
    if (!r) return '';
    const k = r.lang + (r.region ? '-' + r.region : '');
    return hasOwn(PLURALS, k) ? k : hasOwn(PLURALS, r.lang) ? r.lang : '';
  }
  function pluralFn(locale) {
    const r = parseTag(locale) || { lang: 'en', region: '' };
    const k = r.lang + (r.region ? '-' + r.region : '');
    return PLURALS[k] || PLURALS[r.lang] || PLURALS.en.map((x, i) => (i === 1 ? OTHER : i === 3 ? 'other' : x));
  }
  // plural category of a formatted decimal string
  const pluralOf = (locale, s, ordinal, e) => pluralFn(locale)[ordinal ? 1 : 0](operands(s, e));
  // CLDR plural ranges: mostly the end's category; "other – one" is "other" in these
  const RANGE_OTHER = new Set(['en', 'es', 'sv', 'nb', 'no', 'fi', 'fr', 'pt']);
  const pluralRange = (locale, a, b) => (a === 'other' && b === 'one' && RANGE_OTHER.has(String(locale).split('-')[0]) ? 'other' : b);

  // ---------- decimal arithmetic ----------
  // {neg, d: digits without leading/trailing zeros ('' = 0), e: exponent of the last digit}
  function decFromString(s) {
    const m = /^([+-]?)(\d*)(?:\.(\d*))?(?:e([+-]?\d+))?$/i.exec(s);
    if (!m || (!m[2] && !m[3])) return null;
    let digits = (m[2] || '') + (m[3] || '');
    let e = (m[4] ? Number(m[4]) : 0) - (m[3] || '').length;
    digits = digits.replace(/^0+/, '');
    const t = /0*$/.exec(digits)[0].length;
    if (t) { digits = digits.slice(0, digits.length - t); e += t; }
    return { neg: m[1] === '-', d: digits, e: digits ? e : 0 };
  }
  function toDecimal(x) {
    // number | bigint | string(decimal) → decimal, or {special: NaN|±Infinity}
    if (typeof x === 'bigint') return decFromString(String(x));
    if (typeof x === 'string') {
      const s = x.replace(/^[\s\uFEFF\u00A0]+|[\s\uFEFF\u00A0]+$/g, '');
      if (s === '') return { neg: false, d: '', e: 0 };
      if (/^[+-]?Infinity$/.test(s)) return { special: s[0] === '-' ? -Infinity : Infinity };
      if (/^0[xob]/i.test(s)) return decFromString(String(BigInt(s)));
      const d = decFromString(s);
      if (!d) return { special: NaN };
      if (!Number.isFinite(Number(s))) return { special: d.neg ? -Infinity : Infinity };
      return d;
    }
    if (Number.isNaN(x) || !Number.isFinite(x)) return { special: x };
    const d = decFromString(String(Math.abs(x)));
    d.neg = x < 0 || Object.is(x, -0);
    return d;
  }
  const mag = (x) => (x.d ? x.d.length - 1 + x.e : 0);
  function incDigits(s) {
    let i = s.length - 1;
    while (i >= 0 && s[i] === '9') i--;
    if (i < 0) return '1' + '0'.repeat(s.length);
    return s.slice(0, i) + String.fromCharCode(s.charCodeAt(i) + 1) + '0'.repeat(s.length - i - 1);
  }
  function norm(neg, d, e) {
    d = d.replace(/^0+/, '');
    const t = /0*$/.exec(d)[0].length;
    if (t) { d = d.slice(0, d.length - t); e += t; }
    return { neg, d, e: d ? e : 0 };
  }
  function roundUp(mode, neg, half, nonzero, odd) {
    switch (mode) {
      case 'ceil': return nonzero && !neg;
      case 'floor': return nonzero && neg;
      case 'expand': return nonzero;
      case 'trunc': return false;
      case 'halfCeil': return half > 0 || (half === 0 && !neg);
      case 'halfFloor': return half > 0 || (half === 0 && neg);
      case 'halfTrunc': return half > 0;
      case 'halfEven': return half > 0 || (half === 0 && odd);
      default: return half >= 0;
    }
  }
  // keep digits at 10^pos and above
  function roundAt(x, pos, mode, inc) {
    if (inc && inc !== 1) return roundInc(x, pos, mode, inc);
    if (!x.d || x.e >= pos) return x;
    const cut = pos - x.e, n = x.d.length;
    let kept, first, rest;
    if (cut > n) { kept = ''; first = 0; rest = true; }
    else { kept = x.d.slice(0, n - cut); first = x.d.charCodeAt(n - cut) - 48; rest = /[1-9]/.test(x.d.slice(n - cut + 1)); }
    const half = first > 5 || (first === 5 && rest) ? 1 : first === 5 ? 0 : -1;
    const odd = kept ? (kept.charCodeAt(kept.length - 1) - 48) % 2 === 1 : false;
    if (roundUp(mode, x.neg, half, first > 0 || rest, odd)) kept = incDigits(kept || '0');
    return norm(x.neg, kept, pos);
  }
  function roundInc(x, pos, mode, inc) {
    // multiples of inc × 10^pos
    let N = BigInt(x.d || '0'), D = BigInt(inc);
    if (x.e >= pos) N *= 10n ** BigInt(x.e - pos);
    else D *= 10n ** BigInt(pos - x.e);
    let q = N / D;
    const r = N % D, twice = 2n * r;
    const half = twice > D ? 1 : twice === D ? 0 : -1;
    if (roundUp(mode, x.neg, half, r !== 0n, q % 2n === 1n)) q += 1n;
    return norm(x.neg, String(q * BigInt(inc)), pos);
  }
  // digits of x as [intString, fracString] with at least minFrac fraction digits
  function digitsOf(x, minInt, minFrac) {
    let int, frac;
    if (!x.d) { int = '0'; frac = ''; }
    else if (x.e >= 0) { int = x.d + '0'.repeat(x.e); frac = ''; }
    else {
      const p = x.d.length + x.e;
      int = p > 0 ? x.d.slice(0, p) : '0';
      frac = p > 0 ? x.d.slice(p) : '0'.repeat(-p) + x.d;
    }
    if (int.length < minInt) int = '0'.repeat(minInt - int.length) + int;
    else if (minInt === 0 && int === '0') int = '';
    if (frac.length < minFrac) frac += '0'.repeat(minFrac - frac.length);
    return [int, frac];
  }

  // ---------- NumberFormat ----------
  const SIMPLE_UNITS = new Set(UNITS.split(' '));
  const CURRENCY_DIGITS = (c) => (hasOwn(CUR_DIGITS, c) ? CUR_DIGITS[c] : 2);
  const ROUNDING_INCREMENTS = [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000];
  const ROUNDING_MODES = ['ceil', 'floor', 'expand', 'trunc', 'halfCeil', 'halfFloor', 'halfExpand', 'halfTrunc', 'halfEven'];
  const isUnit = (u) => {
    if (SIMPLE_UNITS.has(u)) return true;
    const i = u.indexOf('-per-');
    return i > 0 && SIMPLE_UNITS.has(u.slice(0, i)) && SIMPLE_UNITS.has(u.slice(i + 5));
  };

  function digitOptions(s, o, mnfdDefault, mxfdDefault, notation) {
    s.minimumIntegerDigits = getNum(o, 'minimumIntegerDigits', 1, 21, 1);
    let mnfd = o.minimumFractionDigits, mxfd = o.maximumFractionDigits, mnsd = o.minimumSignificantDigits, mxsd = o.maximumSignificantDigits;
    const inc = getNum(o, 'roundingIncrement', 1, 5000, 1);
    if (!ROUNDING_INCREMENTS.includes(inc)) throw rangeError('roundingIncrement value is out of range.');
    const mode = getOpt(o, 'roundingMode', 'string', ROUNDING_MODES, 'halfExpand', 'Intl.NumberFormat');
    const prio = getOpt(o, 'roundingPriority', 'string', ['auto', 'morePrecision', 'lessPrecision'], 'auto', 'Intl.NumberFormat');
    const tzd = getOpt(o, 'trailingZeroDisplay', 'string', ['auto', 'stripIfInteger'], 'auto', 'Intl.NumberFormat');
    if (inc !== 1) mxfdDefault = mnfdDefault;
    s.roundingIncrement = inc; s.roundingMode = mode; s.trailingZeroDisplay = tzd;
    const hasSd = mnsd !== undefined || mxsd !== undefined, hasFd = mnfd !== undefined || mxfd !== undefined;
    let needSd = true, needFd = true;
    if (prio === 'auto') {
      needSd = hasSd;
      if (needSd || (!hasFd && notation === 'compact')) needFd = false;
    }
    const dflt = (v, min, max, fb, name) => {
      if (v === undefined) return fb;
      v = Number(v);
      if (Number.isNaN(v) || v < min || v > max) throw rangeError(name + ' value is out of range.');
      return Math.floor(v);
    };
    if (needSd) {
      if (hasSd) {
        mnsd = dflt(mnsd, 1, 21, 1, 'minimumSignificantDigits');
        mxsd = dflt(mxsd, mnsd, 21, 21, 'maximumSignificantDigits');
      } else { mnsd = 1; mxsd = 21; }
      s.minimumSignificantDigits = mnsd; s.maximumSignificantDigits = mxsd;
    }
    if (needFd) {
      if (hasFd) {
        mnfd = dflt(mnfd, 0, 100, undefined, 'minimumFractionDigits');
        mxfd = dflt(mxfd, 0, 100, undefined, 'maximumFractionDigits');
        if (mnfd === undefined) mnfd = Math.min(mnfdDefault, mxfd);
        else if (mxfd === undefined) mxfd = Math.max(mxfdDefault, mnfd);
        else if (mnfd > mxfd) throw rangeError('maximumFractionDigits value is out of range.');
      } else { mnfd = mnfdDefault; mxfd = mxfdDefault; }
      s.minimumFractionDigits = mnfd; s.maximumFractionDigits = mxfd;
    }
    if (!needSd && !needFd) {
      s.minimumFractionDigits = 0; s.maximumFractionDigits = 0; s.minimumSignificantDigits = 1; s.maximumSignificantDigits = 2;
      s.roundingType = 'morePrecision'; s.roundingPriority = 'morePrecision';
    } else if (prio === 'auto') {
      s.roundingType = needSd ? 'significantDigits' : 'fractionDigits'; s.roundingPriority = 'auto';
    } else {
      s.roundingType = prio; s.roundingPriority = prio;
    }
    if (inc !== 1) {
      if (s.roundingType !== 'fractionDigits') throw typeError('roundingIncrement can only be used with fractionDigits rounding');
      if (s.maximumFractionDigits !== s.minimumFractionDigits) throw rangeError('maximumFractionDigits must equal minimumFractionDigits with roundingIncrement');
    }
  }

  // round x per the digit options → { x, minFrac }
  function applyRounding(s, x) {
    const mode = s.roundingMode;
    const sig = () => {
      const m = mag(x), pos = m - s.maximumSignificantDigits + 1;
      const r = roundAt(x, pos, mode);
      return { x: r, pos, minFrac: Math.max(0, s.minimumSignificantDigits - 1 - mag(r)) };
    };
    const fix = () => {
      const r = roundAt(x, -s.maximumFractionDigits, mode, s.roundingIncrement);
      return { x: r, pos: -s.maximumFractionDigits, minFrac: s.minimumFractionDigits };
    };
    let res;
    switch (s.roundingType) {
      case 'significantDigits': res = sig(); break;
      case 'fractionDigits': res = fix(); break;
      default: {
        const a = sig(), b = fix();
        res = s.roundingType === 'morePrecision' ? (a.pos <= b.pos ? a : b) : (a.pos > b.pos ? a : b);
      }
    }
    if (s.trailingZeroDisplay === 'stripIfInteger' && res.x.e >= 0) res.minFrac = 0;
    return res;
  }

  function numberSymbols(d) { return d.num; }

  function groupInt(int, s, sym) {
    const g1 = sym.g1, g2 = sym.g2;
    const ug = s.useGrouping;
    if (!ug || int.length <= g1) return [int];
    if (ug === 'min2' || (ug === 'auto' && sym.mg === 2)) { if (int.length < g1 + 2) return [int]; }
    const out = [int.slice(-g1)];
    let rest = int.slice(0, -g1);
    while (rest.length > g2) { out.unshift(rest.slice(-g2)); rest = rest.slice(0, -g2); }
    if (rest) out.unshift(rest);
    return out;
  }

  const mapDigits = (str, zero) => (zero === 0x30 ? str : str.replace(/[0-9]/g, (c) => String.fromCodePoint(zero + c.charCodeAt(0) - 48)));

  // parse a shape like "-¤ #" into tokens
  function shapeTokens(p) {
    const out = [];
    let lit = '';
    for (let i = 0; i < p.length; i++) {
      const c = p[i];
      if (c === "'") { lit += p[++i]; continue; }
      if ('#-+%¤CU'.includes(c)) {
        if (lit) { out.push(lit); lit = ''; }
        out.push({ t: c });
      } else lit += c;
    }
    if (lit) out.push(lit);
    return out;
  }
  const shapeCache = new Map();
  const shape = (p) => { let t = shapeCache.get(p); if (!t) { t = shapeTokens(p); shapeCache.set(p, t); } return t; };

  // locale unit pattern lookup: [pattern for plural cat]
  function unitPattern(d, unit, disp, cat) {
    const u = d.num.unit[unit] || LD('en').num.unit[unit];
    if (!u) return null;
    let e = u[disp[0]] || (disp === 'narrow' ? u.s : null) || u.s || u.l;
    return plv(d.num.unit[unit] ? d : LD('en'), e, cat);
  }
  // a value from plural-packed data (arrays in the locale's category order)
  function plv(d, x, cat) {
    if (!Array.isArray(x) || typeof x[0] === 'number' || (x[0] === null && typeof x[1] === 'string')) return x;
    const cats = d.cats || (d.cats = pluralFn(d.key)[2].split(' '));
    const i = cats.indexOf(cat);
    return (i >= 0 && x[i]) || x[x.length - 1];
  }

  const SPACE_RE = /^[\s\u00A0\u202F]+$/;
  let symbolRE;
  const isSym = (c) => (symbolRE || (symbolRE = U(String.raw`[\p{S}\p{Z}]`))).test(c);

  class NumberFormatImpl {
    constructor(locales, options) {
      const o = coerceOptions(options);
      const s = this;
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.NumberFormat');
      let nu = getOpt(o, 'numberingSystem', 'string', null, undefined);
      if (nu !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(nu)) throw rangeError('Invalid numberingSystem : ' + nu);
      if (nu !== undefined && !hasOwn(NUMBERING, nu)) nu = undefined;
      const r = resolveLocale(locales, ['nu'], { nu });
      s.locale = r.locale; s.key = r.key; s.dataLocale = r.base;
      s.numberingSystem = r.ext.nu || 'latn';
      s.zero = NUMBERING[s.numberingSystem];
      s.style = getOpt(o, 'style', 'string', ['decimal', 'percent', 'currency', 'unit'], 'decimal', 'Intl.NumberFormat');
      let cur = getOpt(o, 'currency', 'string', null, undefined);
      if (cur !== undefined && !/^[A-Za-z]{3}$/.test(cur)) throw rangeError('Invalid currency code : ' + cur);
      const cd = getOpt(o, 'currencyDisplay', 'string', ['code', 'symbol', 'narrowSymbol', 'name'], 'symbol', 'Intl.NumberFormat');
      const cs = getOpt(o, 'currencySign', 'string', ['standard', 'accounting'], 'standard', 'Intl.NumberFormat');
      const unit = getOpt(o, 'unit', 'string', null, undefined);
      if (unit !== undefined && !isUnit(unit)) throw rangeError("Invalid unit argument for Intl.NumberFormat() '" + unit + "'");
      const ud = getOpt(o, 'unitDisplay', 'string', ['short', 'narrow', 'long'], 'short', 'Intl.NumberFormat');
      if (s.style === 'currency') {
        if (cur === undefined) throw typeError('Currency code is required with currency style.');
        s.currency = cur.toUpperCase(); s.currencyDisplay = cd; s.currencySign = cs;
      }
      if (s.style === 'unit') {
        if (unit === undefined) throw typeError('Unit is required with unit style.');
        s.unit = unit; s.unitDisplay = ud;
      }
      const notation = getOpt(o, 'notation', 'string', ['standard', 'scientific', 'engineering', 'compact'], 'standard', 'Intl.NumberFormat');
      let mnfd = 0, mxfd = 3;
      if (s.style === 'currency' && notation === 'standard') { mnfd = mxfd = CURRENCY_DIGITS(s.currency); }
      else if (s.style === 'currency') { mnfd = 0; mxfd = 0; }
      else if (s.style === 'percent') { mnfd = 0; mxfd = 0; }
      if (s.style === 'currency' && notation !== 'standard') { mnfd = mxfd = CURRENCY_DIGITS(s.currency); }
      digitOptions(s, o, mnfd, mxfd, notation);
      s.notation = notation;
      s.compactDisplay = getOpt(o, 'compactDisplay', 'string', ['short', 'long'], 'short', 'Intl.NumberFormat');
      const dg = notation === 'compact' ? 'min2' : 'auto';
      let ug = o.useGrouping;
      if (ug === undefined) ug = dg;
      else if (ug === true) ug = 'always';
      else if (!ug) ug = false;
      else {
        ug = String(ug);
        if (ug === 'true' || ug === 'false') ug = dg;
        else if (!['min2', 'auto', 'always'].includes(ug)) throw rangeError('Value ' + ug + ' out of range for Intl.NumberFormat options property useGrouping');
      }
      s.useGrouping = ug;
      s.signDisplay = getOpt(o, 'signDisplay', 'string', ['auto', 'never', 'always', 'exceptZero', 'negative'], 'auto', 'Intl.NumberFormat');
      s.d = LD(s.key);
    }

    resolved() {
      const s = this, r = { locale: s.locale, numberingSystem: s.numberingSystem, style: s.style };
      if (s.style === 'currency') { r.currency = s.currency; r.currencyDisplay = s.currencyDisplay; r.currencySign = s.currencySign; }
      if (s.style === 'unit') { r.unit = s.unit; r.unitDisplay = s.unitDisplay; }
      r.minimumIntegerDigits = s.minimumIntegerDigits;
      if (s.minimumFractionDigits !== undefined) { r.minimumFractionDigits = s.minimumFractionDigits; r.maximumFractionDigits = s.maximumFractionDigits; }
      if (s.minimumSignificantDigits !== undefined) { r.minimumSignificantDigits = s.minimumSignificantDigits; r.maximumSignificantDigits = s.maximumSignificantDigits; }
      r.useGrouping = s.useGrouping; r.notation = s.notation;
      if (s.notation === 'compact') r.compactDisplay = s.compactDisplay;
      r.signDisplay = s.signDisplay; r.roundingIncrement = s.roundingIncrement; r.roundingMode = s.roundingMode;
      r.roundingPriority = s.roundingPriority; r.trailingZeroDisplay = s.trailingZeroDisplay;
      return r;
    }

    // → { parts: [{type, value}], num: index range of the number core, cat }
    toParts(x, forceCat, approx) {
      const s = this, d = s.d, sym = d.num;
      const dec = toDecimal(x);
      const parts = [];
      let neg, isZero, numParts, compact = null, cat = 'other', numStr = '';
      if (dec.special !== undefined) {
        const v = dec.special;
        neg = v < 0;
        isZero = false;
        numParts = [Number.isNaN(v) ? { type: 'nan', value: sym.nan } : { type: 'infinity', value: sym.inf }];
        if (Number.isNaN(v)) neg = false;
      } else {
        let v = dec;
        if (s.style === 'percent') v = { neg: v.neg, d: v.d, e: v.d ? v.e + 2 : 0 };
        let expo = null;
        if (s.notation === 'scientific' || s.notation === 'engineering') {
          const step = s.notation === 'engineering' ? 3 : 1;
          let ex = v.d ? Math.floor(mag(v) / step) * step : 0;
          let m = { neg: v.neg, d: v.d, e: v.e - ex };
          let rr = applyRounding(s, m);
          if (rr.x.d && mag(rr.x) >= step) { ex += step; m = { neg: v.neg, d: v.d, e: v.e - ex }; rr = applyRounding(s, m); }
          v = rr; expo = ex;
        } else if (s.notation === 'compact') {
          const table = s.compactDisplay === 'long' ? sym.cl : sym.cs;
          const pick = (m) => (m < 3 ? null : table[Math.min(m, 14) - 3]);
          let m = mag(v);
          let rr, ent;
          for (let tries = 0; tries < 2; tries++) {
            ent = pick(m);
            const main = ent && !Array.isArray(ent) ? ent.p : ent;
            const one = main && typeof main[0] !== 'number' ? main.find((x) => x && typeof x[0] === 'number') : main;
            const div = one ? one[0] : 0;
            const sc = { neg: v.neg, d: v.d, e: v.d ? v.e - div : 0 };
            rr = applyRounding(s, sc);
            rr.div = div;
            if (rr.x.d && mag(rr.x) + div > m && m >= 3 - 1 && tries === 0) { m = mag(rr.x) + div; continue; }
            break;
          }
          v = rr; compact = ent; v.compactDiv = rr.div;
        } else v = applyRounding(s, v);
        const r = v.x;
        neg = r.neg;
        isZero = !r.d;
        const [int, frac] = digitsOf(r, s.minimumIntegerDigits, v.minFrac);
        numStr = int + (frac ? '.' + frac : '');
        numParts = [];
        if (int) for (const [i, g] of groupInt(int, s, sym).entries()) {
          if (i) numParts.push({ type: 'group', value: sym.grp });
          numParts.push({ type: 'integer', value: mapDigits(g, s.zero) });
        }
        if (frac) { numParts.push({ type: 'decimal', value: sym.dec }); numParts.push({ type: 'fraction', value: mapDigits(frac, s.zero) }); }
        if (expo !== null) {
          numParts.push({ type: 'exponentSeparator', value: sym.exp });
          if (expo < 0) numParts.push({ type: 'exponentMinusSign', value: sym.min });
          numParts.push({ type: 'exponentInteger', value: mapDigits(String(Math.abs(expo)), s.zero) });
        }
        cat = forceCat || pluralOf(s.dataLocale, numStr, false, compact ? v.compactDiv : 0);
        if (compact && !Array.isArray(compact)) compact = numStr === '1' ? compact.x : compact.p;
        if (compact) compact = plv(d, compact, cat);
        if (compact === null && v.compactDiv) compact = null;
      }
      // sign
      let sign = '';
      switch (s.signDisplay) {
        case 'auto': sign = neg ? '-' : ''; break;
        case 'always': sign = neg ? '-' : '+'; break;
        case 'exceptZero': sign = isZero ? '' : neg ? '-' : '+'; break;
        case 'negative': sign = neg && !isZero ? '-' : ''; break;
        default: sign = '';
      }
      if (dec.special !== undefined && Number.isNaN(dec.special)) sign = s.signDisplay === 'always' ? '+' : '';
      // pattern
      let pat;
      const P = sym.pat;
      const st = s.style;
      let pp = st === 'percent' ? P.p : st === 'currency' ? (s.currencyDisplay === 'name' ? P.cn : P.c) : P.d;
      if (st === 'currency' && s.currencySign === 'accounting' && s.currencyDisplay !== 'name') {
        pat = sign === '-' ? P.ca[1] : sign === '+' ? pp[1].replace('-', '+') : P.ca[0];
      } else pat = sign === '-' ? pp[1] : sign === '+' ? pp[1].replace('-', '+') : pp[0];
      // formatRange of equal ends: the approximately sign sits where the minus sign goes
      if (approx) pat = st === 'currency' && s.currencySign === 'accounting' && sign === '-' ? '~' + pat : pp[1].replace('-', sign === '-' ? "'\u0000-" : sign === '+' ? "'\u0000+" : "'\u0000");
      let toks = shape(pat);
      if (compact && compact[1]) {
        const ct = shape(compact[1]);
        toks = toks.flatMap((t) => (t.t === '#' ? ct : [t]));
      }
      // currency text
      let curText = '';
      if (st === 'currency') {
        // [names, symbol, narrow]; outside English a missing (0) symbol is the English one
        const c = s.currency, ent = (d.num.cur && d.num.cur[c]) || null;
        const en = LD('en'), ee = en.num.cur[c] || null;
        const isEn = s.key.startsWith('en');
        const symOf = (e) => (e ? (e.length > 1 ? e[1] : c) : c);
        const sym1 = isEn ? symOf(ent) : ent && ent[1] ? ent[1] : symOf(ee);
        if (s.currencyDisplay === 'code') curText = c;
        else if (s.currencyDisplay === 'name') {
          const nm = ent && ent[0] ? plv(d, ent[0], cat) : ee && ee[0] ? plv(en, ee[0], cat === 'one' ? 'one' : 'other') : c;
          curText = nm || c;
        } else if (s.currencyDisplay === 'narrowSymbol') {
          curText = isEn ? (ent ? ent[2] || symOf(ent) : narrowFallback(c)) : ent && ent[2] ? ent[2] : ee ? ee[2] || (ent && ent[1]) || symOf(ee) : narrowFallback(c);
        } else curText = sym1;
      }
      // assemble
      const out = [];
      let numStart = -1, numEnd = -1;
      for (let i = 0; i < toks.length; i++) {
        const t = toks[i];
        if (typeof t === 'string') {
          if (t.includes('\u0000')) {
            const [a, b] = t.split('\u0000');
            if (a) out.push({ type: 'literal', value: a });
            out.push({ type: 'approximatelySign', value: sym.approx });
            if (b) out.push({ type: 'literal', value: b });
            continue;
          }
          out.push({ type: 'literal', value: t });
          continue;
        }
        switch (t.t) {
          case '#': numStart = out.length; out.push(...numParts); numEnd = out.length; break;
          case '-': out.push({ type: 'minusSign', value: sym.min }); break;
          case '+': out.push({ type: 'plusSign', value: sym.plus }); break;
          case '%': out.push({ type: 'percentSign', value: sym.pct }); break;
          case 'C': out.push({ type: 'compact', value: compact[2] }); break;
          case '¤': {
            // currency spacing: a letter-like symbol touching the number gets a no-break space
            const prev = toks[i - 1], next = toks[i + 1];
            if (s.currencyDisplay !== 'name' && numParts[0].type === 'integer') {
              if (next && next.t === '#' && !isSym(curText[curText.length - 1])) { out.push({ type: 'currency', value: curText }); out.push({ type: 'literal', value: '\u00A0' }); break; }
              if (prev && prev.t === '#' && !isSym(curText[0])) { out.push({ type: 'literal', value: '\u00A0' }); out.push({ type: 'currency', value: curText }); break; }
            }
            out.push({ type: 'currency', value: curText });
            break;
          }
        }
      }
      if (st === 'unit') return this.unitWrap(out, numStart, numEnd, cat);
      return { parts: mergeLiterals(out), cat };
    }

    unitWrap(inner, a, b, cat) {
      const s = this, d = s.d;
      let pat = unitPattern(d, s.unit, s.unitDisplay, cat);
      if (!pat) {
        const i = s.unit.indexOf('-per-');
        const u1 = s.unit.slice(0, i), u2 = s.unit.slice(i + 5);
        const p1 = unitPattern(d, u1, s.unitDisplay, cat);
        const p2 = unitPattern(d, u2, s.unitDisplay, 'one');
        const name2 = p2.replace('{0}', '').trim();
        pat = s.unitDisplay === 'long' ? p1 + ' per ' + name2 : p1 + '/' + name2;
        if (s.unitDisplay !== 'long') pat = pat.replace(/\s+\//, '/');
      }
      const i = pat.indexOf('{0}');
      const out = [];
      const pushText = (t) => {
        // whitespace → literal, the rest → unit
        const re = /[\s\u00A0\u202F]+|[^\s\u00A0\u202F]+/g;
        let m;
        while ((m = re.exec(t))) out.push({ type: SPACE_RE.test(m[0]) ? 'literal' : 'unit', value: m[0] });
      };
      pushText(pat.slice(0, i));
      out.push(...inner);
      pushText(pat.slice(i + 3));
      return { parts: mergeLiterals(out), cat };
    }

    format(x) { const p = this.toParts(x).parts; let s = ''; for (const q of p) s += q.value; return s; }

    rangeParts(x, y) {
      // ICU's range rules: the outer modifier (unit or currency name) is shared
      // when both ends have it; the middle one (sign, symbol, %, compact suffix)
      // only when equal and longer than one code point. Repeated modifiers get
      // spaces around the range separator.
      const A = this.toParts(x);
      let B = this.toParts(y);
      // CLDR plural ranges ("1–2 days", "-5 – -1 km")
      const rc = pluralRange(this.dataLocale, A.cat, B.cat);
      if (rc !== B.cat) B = this.toParts(y, rc);
      const sym = this.d.num;
      const txt = (ps) => ps.map((p) => p.value).join('');
      const tag = (ps, src) => ps.map((p) => ({ type: p.type, value: p.value, source: src }));
      if (txt(A.parts) === txt(B.parts)) return tag(this.toParts(x, undefined, true).parts, 'shared');
      const isNum = (p) => ['integer', 'group', 'decimal', 'fraction', 'nan', 'infinity', 'exponentSeparator', 'exponentMinusSign', 'exponentInteger'].includes(p.type);
      const outerType = this.style === 'unit' ? 'unit' : this.style === 'currency' && this.currencyDisplay === 'name' ? 'currency' : null;
      // [outerPre, middlePre, number, middlePost, outerPost]
      const split = (ps) => {
        let i = 0, j = ps.length;
        while (i < j && !isNum(ps[i])) i++;
        while (j > i && !isNum(ps[j - 1])) j--;
        const pre = ps.slice(0, i), num = ps.slice(i, j), post = ps.slice(j);
        let op = 0, oq = post.length;
        if (outerType) {
          // outer: the unit/name text plus the whitespace that joins it
          let k = 0;
          while (k < pre.length && (pre[k].type === outerType || (pre[k].type === 'literal' && SPACE_RE.test(pre[k].value) && k > 0))) k++;
          op = pre.some((p) => p.type === outerType) ? k : 0;
          let m = post.length;
          while (m > 0 && (post[m - 1].type === outerType || (post[m - 1].type === 'literal' && SPACE_RE.test(post[m - 1].value) && m < post.length))) m--;
          oq = post.some((p) => p.type === outerType) ? m : post.length;
        }
        return [pre.slice(0, op), pre.slice(op), num, post.slice(0, oq), post.slice(oq)];
      };
      const a = split(A.parts), b = split(B.parts);
      const rp = sym.rng, i0 = rp.indexOf('{0}'), i1 = rp.indexOf('{1}');
      let sep = rp.slice(i0 + 3, i1);
      const shapeOf = (ps) => ps.map((p) => p.type + (p.type === outerType ? '' : ':' + p.value)).join('|');
      const outerSame = !!outerType && (a[0].length || a[4].length) && shapeOf(a[0]) === shapeOf(b[0]) && shapeOf(a[4]) === shapeOf(b[4]);
      const midA = txt(a[1]) + txt(a[3]);
      const midSame = outerSame || !outerType ? txt(a[1]) === txt(b[1]) && txt(a[3]) === txt(b[3]) && [...midA].length > 1 : false;
      const repeated = (!midSame && midA) || (!outerSame && (a[0].length || a[4].length));
      if (repeated) {
        if (!/^\s/.test(sep)) sep = ' ' + sep;
        if (!/\s$/.test(sep)) sep = sep + ' ';
      }
      const S = { type: 'literal', value: sep, source: 'shared' };
      let out;
      if (midSame) out = [...tag(b[0], 'shared'), ...tag(a[1], 'shared'), ...tag(a[2], 'startRange'), S, ...tag(b[2], 'endRange'), ...tag(b[3], 'shared'), ...tag(b[4], 'shared')];
      else if (outerSame) out = [...tag(b[0], 'shared'), ...tag([...a[1], ...a[2], ...a[3]], 'startRange'), S, ...tag([...b[1], ...b[2], ...b[3]], 'endRange'), ...tag(b[4], 'shared')];
      else out = [...tag(A.parts, 'startRange'), S, ...tag(B.parts, 'endRange')];
      return mergeLiterals(out, true);
    }
  }
  const NARROW_CUR = { USD: '$', EUR: '€', GBP: '£', JPY: '¥', CNY: '¥', INR: '₹', CAD: '$', AUD: '$', NZD: '$', KRW: '₩', RUB: '₽', BRL: 'R$', MXN: '$', HKD: '$', SGD: '$', TWD: '$', ILS: '₪', TRY: '₺', UAH: '₴', VND: '₫', THB: '฿', PHP: '₱', NGN: '₦', PLN: 'zł', SEK: 'kr', NOK: 'kr', DKK: 'kr', CHF: 'CHF', ZAR: 'R' };
  const narrowFallback = (c) => NARROW_CUR[c] || c;

  function mergeLiterals(ps, keepSource) {
    const out = [];
    for (const p of ps) {
      const last = out[out.length - 1];
      if (last && p.type === 'literal' && last.type === 'literal' && (!keepSource || last.source === p.source)) last.value += p.value;
      else if (p.value !== '' || p.type !== 'literal') out.push(keepSource || p.source === undefined ? p : p);
    }
    return out;
  }

  // ToIntlMathematicalValue
  function toIntlMV(v) {
    if (typeof v === 'bigint' || typeof v === 'number') return v;
    if (typeof v === 'symbol') throw typeError('Cannot convert a Symbol value to a number');
    if (typeof v === 'object' && v !== null) {
      const p = toPrimitiveNumber(v);
      if (typeof p === 'bigint' || typeof p === 'string') return p;
      return Number(p);
    }
    if (typeof v === 'string') return v;
    return Number(v);
  }
  function toPrimitiveNumber(o) {
    const ex = o[Symbol.toPrimitive];
    if (ex !== undefined && ex !== null) {
      const r = ex.call(o, 'number');
      if (typeof r !== 'object' || r === null) return r;
      throw typeError('Cannot convert object to primitive value');
    }
    for (const m of ['valueOf', 'toString']) {
      const f = o[m];
      if (typeof f === 'function') {
        const r = f.call(o);
        if (typeof r !== 'object' || r === null) return r;
      }
    }
    throw typeError('Cannot convert object to primitive value');
  }

  // ---------- time zones ----------
  let zoneLC, zoneMeta;
  function zoneTables() {
    if (zoneLC) return;
    zoneLC = new Map();
    zoneMeta = new Map();
    const z = ZONES.split(' '), m = ZONE_META.split(' ');
    for (let i = 0; i < z.length; i++) {
      zoneLC.set(z[i].toLowerCase(), z[i]);
      if (m[i]) zoneMeta.set(z[i], parseInt(m[i], 36));
    }
    for (const [k, v] of Object.entries(ZONE_LINKS)) zoneLC.set(k.toLowerCase(), v);
  }
  // modern IANA names and old links → the ICU canonical id
  const ZONE_LINKS = {
    'Asia/Kolkata': 'Asia/Calcutta', 'Asia/Ho_Chi_Minh': 'Asia/Saigon', 'Asia/Kathmandu': 'Asia/Katmandu', 'Asia/Yangon': 'Asia/Rangoon', 'Europe/Kyiv': 'Europe/Kiev',
    'America/Argentina/Buenos_Aires': 'America/Buenos_Aires', 'America/Indiana/Indianapolis': 'America/Indianapolis', 'America/Kentucky/Louisville': 'America/Louisville', 'America/Nuuk': 'America/Godthab',
    'Atlantic/Faroe': 'Atlantic/Faeroe', 'Pacific/Chuuk': 'Pacific/Truk', 'Pacific/Pohnpei': 'Pacific/Ponape', 'Asia/Dacca': 'Asia/Dhaka', 'Asia/Thimbu': 'Asia/Thimphu',
    'US/Pacific': 'America/Los_Angeles', 'US/Eastern': 'America/New_York', 'US/Central': 'America/Chicago', 'US/Mountain': 'America/Denver', 'US/Arizona': 'America/Phoenix', 'US/Alaska': 'America/Anchorage', 'US/Hawaii': 'Pacific/Honolulu', 'US/Aleutian': 'America/Adak', 'US/East-Indiana': 'America/Indianapolis', 'US/Michigan': 'America/Detroit',
    'Canada/Pacific': 'America/Vancouver', 'Canada/Eastern': 'America/Toronto', 'Canada/Central': 'America/Winnipeg', 'Canada/Mountain': 'America/Edmonton', 'Canada/Atlantic': 'America/Halifax', 'Canada/Newfoundland': 'America/St_Johns',
    'Europe/Belfast': 'Europe/London', GB: 'Europe/London', 'GB-Eire': 'Europe/London', Eire: 'Europe/Dublin', Portugal: 'Europe/Lisbon', Poland: 'Europe/Warsaw', Turkey: 'Europe/Istanbul', 'Asia/Istanbul': 'Europe/Istanbul', 'W-SU': 'Europe/Moscow',
    Japan: 'Asia/Tokyo', PRC: 'Asia/Shanghai', ROK: 'Asia/Seoul', ROC: 'Asia/Taipei', Hongkong: 'Asia/Hong_Kong', Singapore: 'Asia/Singapore', Israel: 'Asia/Jerusalem', 'Asia/Tel_Aviv': 'Asia/Jerusalem', Iran: 'Asia/Tehran', Egypt: 'Africa/Cairo', Libya: 'Africa/Tripoli',
    NZ: 'Pacific/Auckland', 'Australia/ACT': 'Australia/Sydney', 'Australia/NSW': 'Australia/Sydney', 'Australia/Victoria': 'Australia/Melbourne', 'Australia/Queensland': 'Australia/Brisbane', 'Australia/West': 'Australia/Perth', 'Australia/South': 'Australia/Adelaide', 'Australia/North': 'Australia/Darwin', 'Australia/Tasmania': 'Australia/Hobart',
    'Brazil/East': 'America/Sao_Paulo', 'Mexico/General': 'America/Mexico_City', 'Chile/Continental': 'America/Santiago', Cuba: 'America/Havana', Jamaica: 'America/Jamaica', Navajo: 'America/Denver', Iceland: 'Atlantic/Reykjavik',
  };
  const UTC_NAMES = new Set(['utc', 'etc/utc', 'etc/gmt', 'gmt', 'etc/uct', 'uct', 'etc/universal', 'universal', 'etc/zulu', 'zulu', 'etc/greenwich', 'greenwich', 'gmt0', 'etc/gmt0', 'gmt+0', 'gmt-0', 'etc/gmt+0', 'etc/gmt-0']);
  const zoneCache = new Map();
  // → { id: resolved name, host: name for the host, fixed: minutes|null }
  function resolveZone(tz) {
    if (tz === undefined) {
      let host = String(H.localZone || '');
      if (host === 'Local') host = '';
      const k = '\0local';
      let z = zoneCache.get(k);
      if (z) return z;
      let id;
      try { id = H.tz(host, Date.now()).name; } catch { host = ''; id = H.tz('', Date.now()).name; }
      zoneTables();
      id = zoneLC.get(String(id).toLowerCase()) || id;
      if (UTC_NAMES.has(String(id).toLowerCase())) id = 'UTC';
      z = { id, host, fixed: null, local: true };
      // the host may not know the local zone's name (Go reports "Local" → "UTC"):
      // then only offsets are trustworthy
      if (id === 'UTC' && offsetAt(z, Date.now()) !== 0) z.anon = true;
      zoneCache.set(k, z);
      return z;
    }
    const s = String(tz);
    let z = zoneCache.get(s);
    if (z) return z;
    const m = /^([+\-−])(\d{2})(?::?(\d{2}))?$/.exec(s);
    if (m) {
      const h = +m[2], mi = +(m[3] || 0);
      if (h > 23 || mi > 59) throw rangeError('Invalid time zone specified: ' + s);
      const neg = m[1] !== '+';
      z = { id: (neg ? '-' : '+') + m[2] + ':' + (m[3] || '00'), host: '', fixed: (neg ? -1 : 1) * (h * 60 + mi) };
    } else {
      const lc = s.toLowerCase();
      if (UTC_NAMES.has(lc)) z = { id: 'UTC', host: 'UTC', fixed: 0 };
      else {
        zoneTables();
        let id = zoneLC.get(lc);
        const eg = /^etc\/gmt([+-])(\d{1,2})$/.exec(lc);
        if (!id && eg && +eg[2] <= 14) id = 'Etc/GMT' + eg[1] + eg[2];
        let host = id || s;
        try { const r = H.tz(host, Date.now()); if (!id) id = r.name || s; }
        catch {
          if (!id) throw rangeError('Invalid time zone specified: ' + s);
        }
        z = { id, host, fixed: null };
      }
    }
    if (zoneCache.size > 500) zoneCache.clear();
    zoneCache.set(s, z);
    return z;
  }
  const offCache = new Map();
  function offsetAt(z, ms) {
    if (z.fixed !== null) return z.fixed;
    const k = z.host + '|' + Math.floor(ms / 900000);
    let o = offCache.get(k);
    if (o === undefined) {
      try { o = H.tz(z.host, ms).offset; } catch { o = 0; }
      if (offCache.size > 4000) offCache.clear();
      offCache.set(k, o);
    }
    return o;
  }
  const stdCache = new Map();
  function isDst(z, ms, off) {
    if (z.fixed !== null) return false;
    const y = new Date(ms).getUTCFullYear();
    const k = z.host + '|' + y;
    let std = stdCache.get(k);
    if (std === undefined) {
      std = Math.min(offsetAt(z, Date.UTC(y, 0, 1)), offsetAt(z, Date.UTC(y, 6, 1)));
      stdCache.set(k, std);
    }
    return off > std;
  }
  function gmtFormat(d, off, long) {
    const g = d.gmt;
    if (off === 0 && !long) return g[1];
    const neg = off < 0, a = Math.abs(off), h = Math.floor(a / 60), m = a % 60;
    let f = neg ? g[3] : g[2];
    if (long) f = f.replace('HH', String(h).padStart(2, '0')).replace('mm', String(m).padStart(2, '0'));
    else {
      f = f.replace('HH', String(h));
      f = m ? f.replace('mm', String(m).padStart(2, '0')) : f.replace(/[^Hhm0-9+\-−]*mm/, '');
    }
    return g[0].replace('{0}', f);
  }
  function zoneName(d, key, z, ms, off, style) {
    if (style === 'shortOffset') return gmtFormat(d, off, false);
    if (style === 'longOffset') return gmtFormat(d, off, true);
    if ((z.fixed !== null && z.id !== 'UTC') || z.anon) return gmtFormat(d, off, style === 'long' || style === 'longGeneric');
    if (z.id === 'UTC') {
      if (style === 'short') return d.gmt[4];
      if (style === 'long') return d.gmt[5];
      return gmtFormat(d, 0, style === 'longGeneric');
    }
    zoneTables();
    const meta = zoneMeta.get(z.id);
    const names = (d.zg && d.zg[z.id]) || (meta !== undefined && d.mz && d.mz[meta]) || null;
    if (style === 'shortGeneric') return (d.zs && d.zs[z.id]) || gmtFormat(d, off, false);
    if (!names) return gmtFormat(d, off, style !== 'short');
    const dst = isDst(z, ms, off);
    let v;
    if (style === 'short') v = names[dst ? 1 : 0];
    else if (style === 'long') v = names[dst ? 3 : 2];
    else v = names[4];
    return v || gmtFormat(d, off, style !== 'short');
  }

  // ---------- date patterns ----------
  let ALPHA_IDX;
  const alphaIdx = (c) => {
    if (!ALPHA_IDX) {
      ALPHA_IDX = new Map();
      let n = 0;
      for (let c = 0x30; c < 0x7f; c++) if (c !== 0x5c && c !== 0x60) ALPHA_IDX.set(String.fromCharCode(c), n++);
      for (let c = 0xc0; c < 0x250; c++) ALPHA_IDX.set(String.fromCharCode(c), n++);
    }
    return ALPHA_IDX.get(c);
  };
  const tableGet = (t, i) => t[0][alphaIdx(t[1][i])];
  const patCache = new Map();
  function tokens(p) {
    let t = patCache.get(p);
    if (t) return t;
    t = [];
    const re = /\{([A-Za-z0-9]+)\}/g;
    let last = 0, m;
    while ((m = re.exec(p))) {
      if (m.index > last) t.push(p.slice(last, m.index));
      if (m[1] === 'lb') t.push('{');
      else if (m[1] === 'rb') t.push('}');
      else t.push({ f: m[1][0], w: m[1].slice(1) });
      last = re.lastIndex;
    }
    if (last < p.length) t.push(p.slice(last));
    patCache.set(p, t);
    return t;
  }
  const W_IDX = { long: 1, short: 2, narrow: 3 };
  const M_IDX = { numeric: 1, '2-digit': 2, long: 3, short: 4, narrow: 5 };
  const N_IDX = { numeric: 1, '2-digit': 2 };
  const glueApply = (g, date, time) => g.replace('{1}', '\u0001').replace('{0}', time).replace('\u0001', date);

  // the pattern string for the component options of a format
  function componentPattern(s) {
    const d = s.d;
    const o = s.opts;
    const hasDate = o.weekday || o.era || o.year || o.month || o.day;
    const hasTime = o.hour || o.minute || o.second || o.fractionalSecondDigits || o.dayPeriod;
    const h12 = s.hc === 'h11' || s.hc === 'h12';
    let datePat = '', timePat = '';
    if (hasDate) {
      const k = (((((W_IDX[o.weekday] || 0) * 2 + (o.era ? 1 : 0)) * 3 + (N_IDX[o.year] || 0)) * 6 + (M_IDX[o.month] || 0)) * 3) + (N_IDX[o.day] || 0);
      datePat = tableGet(d.dp, k);
    }
    let tk = -1;
    if (hasTime) {
      let sec = o.second;
      if (o.fractionalSecondDigits && !sec) sec = 'numeric';
      tk = (((N_IDX[o.hour] || 0) * 3 + (N_IDX[o.minute] || 0)) * 3 + (N_IDX[sec] || 0)) * 2 + (o.timeZoneName ? 1 : 0);
      if (!o.hour && !o.minute && !sec) {
        timePat = '{B}' + (o.timeZoneName ? ' {z}' : '');
      } else {
        timePat = tableGet(h12 ? d.tp12 : d.tp23, tk);
        if (o.fractionalSecondDigits) timePat = timePat.replace(/\{s[12]?\}/, (x) => x + d.fs + '{S}');
        if (o.dayPeriod && h12) timePat = timePat.replace('{a}', '{B}');
      }
    } else if (o.timeZoneName) {
      // a time zone name alone rides on the date
      const tz = tableGet(h12 ? d.tp12 : d.tp23, 1);
      void tz;
    }
    const ml = o.month === 'long', glue = ml && o.weekday === 'long' ? d.g.f : ml ? d.g.l : o.month === 'short' ? d.g.m : d.g.s;
    if (!hasTime && o.timeZoneName && datePat) return glueApply(glue, datePat, '{z}');
    if (datePat && timePat) {
      const dateOnlyWeekday = o.weekday && !o.era && !o.year && !o.month && !o.day;
      if (dateOnlyWeekday && tk >= 0 && (o.hour || o.minute || o.second)) {
        const wt = (d.wt[o.weekday[0]] || d.wt.s || d.wt.l)[h12 ? 0 : 1];
        const exc = wt[4];
        let p = exc && exc[tk];
        if (p) {
          if (o.fractionalSecondDigits) p = p.replace(/\{s[12]?\}/, (x) => x + d.fs + '{S}');
          if (o.dayPeriod && h12) p = p.replace('{a}', '{B}');
          return p;
        }
        const cls = (o.minute ? 1 : 0) + (o.timeZoneName ? 2 : 0);
        return glueApply(wt[cls], datePat, timePat);
      }
      return glueApply(glue, datePat, timePat);
    }
    return datePat || timePat;
  }

  function stylePattern(s) {
    const d = s.d, st = d.st;
    const I = { full: 0, long: 1, medium: 2, short: 3 };
    const h12 = s.hc === 'h11' || s.hc === 'h12';
    const dp = s.dateStyle ? st.d[I[s.dateStyle]] : '';
    const tp = s.timeStyle ? (h12 ? st.t12 : st.t23)[I[s.timeStyle]] : '';
    if (dp && tp) {
      const row = st.g[I[s.dateStyle]];
      return glueApply(Array.isArray(row) ? row[I[s.timeStyle]] : row, dp, tp);
    }
    return dp || tp;
  }

  const pad2 = (n) => (n < 10 ? '0' + n : String(n));
  const scratch = new Date(0);
  function fieldsAt(s, ms) {
    const z = s.zone;
    const off = offsetAt(z, ms);
    scratch.setTime(ms + off * 60000);
    return {
      ms, off, y: scratch.getUTCFullYear(), M: scratch.getUTCMonth() + 1, d: scratch.getUTCDate(), wd: scratch.getUTCDay(),
      h: scratch.getUTCHours(), mi: scratch.getUTCMinutes(), s: scratch.getUTCSeconds(), S: scratch.getUTCMilliseconds(),
    };
  }
  const PART = { y: 'year', M: 'month', L: 'month', E: 'weekday', c: 'weekday', G: 'era', d: 'day', h: 'hour', m: 'minute', s: 'second', S: 'fractionalSecond', a: 'dayPeriod', B: 'dayPeriod', z: 'timeZoneName' };
  const WCH = { l: 'long', s: 'short', n: 'narrow' };

  function flexPeriod(n, w, h, mi) {
    if (mi === 0 && (h === 0 || h === 12)) {
      const z = n['B' + w + '0'];
      if (z && z[h ? 1 : 0]) return z[h ? 1 : 0];
    }
    const t = n['B' + w];
    let v = t[1];
    for (let i = 0; i < t.length; i += 2) if (t[i] <= h) v = t[i + 1];
    return v;
  }

  function renderPattern(s, pat, f) {
    const d = s.d, n = d.n, o = s.opts;
    const toks = tokens(pat);
    const out = [];
    const num = (v, w, opt) => {
      const two = w === '2' || (w === '' && opt === '2-digit');
      return mapDigits(two ? pad2(v % 100) : String(v), s.zero);
    };
    for (const t of toks) {
      if (typeof t === 'string') { out.push({ type: 'literal', value: t }); continue; }
      let v;
      const w = t.w;
      switch (t.f) {
        case 'y': {
          const ey = f.y > 0 ? f.y : 1 - f.y;
          const two = w === '2' || (w === '' && o.year === '2-digit');
          v = mapDigits(two ? pad2(ey % 100) : String(ey), s.zero);
          break;
        }
        case 'M': case 'L': {
          const ow = w || o.month || 'numeric';
          if (ow === '1' || ow === 'numeric') v = mapDigits(String(f.M), s.zero);
          else if (ow === '2' || ow === '2-digit') v = mapDigits(pad2(f.M), s.zero);
          else {
            const wc = ow.length === 1 ? ow : ow[0];
            v = ((t.f === 'L' && n['L' + wc]) || n['M' + wc])[f.M - 1];
          }
          break;
        }
        case 'E': case 'c': {
          const wc = w || (o.weekday || 'short')[0];
          v = ((t.f === 'c' && n['c' + wc]) || n['E' + wc])[f.wd];
          break;
        }
        case 'G': { const wc = w || (o.era || 'short')[0]; v = n['G' + wc][f.y > 0 ? 1 : 0]; break; }
        case 'd': v = num(f.d, w, o.day); break;
        case 'h': {
          let h = f.h;
          if (s.hc === 'h12') h = h % 12 || 12;
          else if (s.hc === 'h11') h = h % 12;
          else if (s.hc === 'h24') h = h || 24;
          v = num(h, w, o.hour);
          break;
        }
        case 'm': v = num(f.mi, w, o.minute); break;
        case 's': v = num(f.s, w, o.second); break;
        case 'S': v = mapDigits(String(f.S).padStart(3, '0').slice(0, o.fractionalSecondDigits || 3), s.zero); break;
        case 'a': v = n.a[f.h < 12 ? 0 : 1]; break;
        case 'B': v = flexPeriod(n, (o.dayPeriod || 'short')[0], f.h, f.mi); break;
        case 'z': v = zoneName(d, s.key, s.zone, f.ms, f.off, w ? (w === 'l' ? 'long' : 'short') : o.timeZoneName || 'short'); break;
        default: v = '';
      }
      out.push({ type: PART[t.f], value: v });
    }
    // merge adjacent literals
    const m = [];
    for (const p of out) {
      const last = m[m.length - 1];
      if (last && last.type === 'literal' && p.type === 'literal') last.value += p.value;
      else m.push(p);
    }
    return m;
  }

  // resolved component values from the pattern actually used
  function resolvedComponents(s, pat) {
    const o = s.opts, r = {};
    for (const t of tokens(pat)) {
      if (typeof t === 'string') continue;
      const w = t.w;
      const width = (opt) => (w === '1' ? 'numeric' : w === '2' ? '2-digit' : w && WCH[w] ? WCH[w] : opt);
      switch (t.f) {
        case 'y': r.year = width(o.year || 'numeric'); break;
        case 'M': case 'L': r.month = width(o.month || 'numeric'); break;
        case 'E': case 'c': r.weekday = width(o.weekday || 'short'); break;
        case 'G': r.era = width(o.era || 'short'); break;
        case 'd': r.day = width(o.day || 'numeric'); break;
        case 'h': r.hour = width(o.hour || 'numeric'); break;
        case 'm': r.minute = width(o.minute || 'numeric'); break;
        case 's': r.second = width(o.second || 'numeric'); break;
        case 'B': r.dayPeriod = o.dayPeriod; break;
        case 'z': r.timeZoneName = o.timeZoneName; break;
      }
    }
    return r;
  }

  const DT_FIELDS = ['weekday', 'era', 'year', 'month', 'day', 'dayPeriod', 'hour', 'minute', 'second', 'fractionalSecondDigits', 'timeZoneName'];
  const DT_VALUES = {
    weekday: ['narrow', 'short', 'long'], era: ['narrow', 'short', 'long'], year: ['2-digit', 'numeric'], month: ['2-digit', 'numeric', 'narrow', 'short', 'long'],
    day: ['2-digit', 'numeric'], dayPeriod: ['narrow', 'short', 'long'], hour: ['2-digit', 'numeric'], minute: ['2-digit', 'numeric'], second: ['2-digit', 'numeric'],
    timeZoneName: ['short', 'long', 'shortOffset', 'longOffset', 'shortGeneric', 'longGeneric'],
  };

  class DateTimeFormatImpl {
    // required: 'date' | 'time' | 'any'; defaults: 'date' | 'time' | 'all'
    constructor(locales, options, required, defaults) {
      const o = coerceOptions(options);
      const s = this;
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.DateTimeFormat');
      let ca = getOpt(o, 'calendar', 'string', null, undefined);
      if (ca !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(ca)) throw rangeError('Invalid calendar : ' + ca);
      if (ca !== undefined) ca = EXT_OK.ca(ca.toLowerCase()) || undefined;
      let nu = getOpt(o, 'numberingSystem', 'string', null, undefined);
      if (nu !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(nu)) throw rangeError('Invalid numberingSystem : ' + nu);
      if (nu !== undefined && !hasOwn(NUMBERING, nu)) nu = undefined;
      const hour12 = getOpt(o, 'hour12', 'boolean', null, undefined);
      let hc = getOpt(o, 'hourCycle', 'string', ['h11', 'h12', 'h23', 'h24'], undefined, 'Intl.DateTimeFormat');
      if (hour12 !== undefined) hc = null;
      const r = resolveLocale(locales, ['ca', 'hc', 'nu'], { ca, hc: hc === null ? undefined : hc, nu });
      s.locale = r.locale; s.key = r.key; s.dataLocale = r.base;
      if (hour12 !== undefined && hasOwn(uKeywords(r.locale), 'hc')) s.locale = s.locale.replace(/-hc-h\d\d/, '').replace(/-u$/, '');
      s.calendar = 'gregory';
      s.numberingSystem = r.ext.nu || 'latn';
      s.zero = NUMBERING[s.numberingSystem];
      s.d = LD(s.key);
      s.zone = resolveZone(o.timeZone);
      s.timeZone = s.zone.id;
      const opts = {};
      for (const f of DT_FIELDS) {
        if (f === 'fractionalSecondDigits') { const v = getNum(o, f, 1, 3, undefined); if (v !== undefined) opts[f] = v; continue; }
        const v = getOpt(o, f, 'string', DT_VALUES[f], undefined, 'Intl.DateTimeFormat');
        if (v !== undefined) opts[f] = v;
      }
      getOpt(o, 'formatMatcher', 'string', ['basic', 'best fit'], 'best fit', 'Intl.DateTimeFormat');
      const ds = getOpt(o, 'dateStyle', 'string', ['full', 'long', 'medium', 'short'], undefined, 'Intl.DateTimeFormat');
      const ts = getOpt(o, 'timeStyle', 'string', ['full', 'long', 'medium', 'short'], undefined, 'Intl.DateTimeFormat');
      if (ds || ts) {
        for (const f of DT_FIELDS) if (opts[f] !== undefined) throw typeError("Can't set option " + f + ' when ' + (ds ? 'dateStyle' : 'timeStyle') + ' is used');
        if (required === 'date' && ts && !ds) throw typeError('Invalid option : timeStyle');
        if (required === 'time' && ds && !ts) throw typeError('Invalid option : dateStyle');
        s.dateStyle = ds; s.timeStyle = ts;
      } else {
        let need = true;
        if (required === 'date' || required === 'any') for (const f of ['weekday', 'year', 'month', 'day']) if (opts[f] !== undefined) need = false;
        if (required === 'time' || required === 'any') for (const f of ['dayPeriod', 'hour', 'minute', 'second', 'fractionalSecondDigits']) if (opts[f] !== undefined) need = false;
        if (need && (defaults === 'date' || defaults === 'all')) opts.year = opts.month = opts.day = 'numeric';
        if (need && (defaults === 'time' || defaults === 'all')) opts.hour = opts.minute = opts.second = 'numeric';
      }
      s.opts = opts;
      // hour cycle
      const hasHour = !!opts.hour || !!ts;
      let cyc = r.ext.hc;
      if (hour12 !== undefined) cyc = hour12 ? s.d.hc12 || 'h12' : 'h23';
      if (!cyc) cyc = s.d.hc || 'h12';
      s.hc = cyc;
      s.hasHour = hasHour;
      s.pattern = ds || ts ? stylePattern(s) : componentPattern(s);
    }
    resolved() {
      const s = this;
      const r = { locale: s.locale, calendar: s.calendar, numberingSystem: s.numberingSystem, timeZone: s.timeZone };
      const comp = s.dateStyle || s.timeStyle ? {} : resolvedComponents(s, s.pattern);
      if (s.hasHour && (s.timeStyle || comp.hour)) { r.hourCycle = s.hc; r.hour12 = s.hc === 'h11' || s.hc === 'h12'; }
      for (const f of DT_FIELDS) {
        if (f === 'fractionalSecondDigits') { if (s.opts[f] && !s.dateStyle && !s.timeStyle) r[f] = s.opts[f]; continue; }
        if (comp[f] !== undefined) r[f] = comp[f];
      }
      if (s.dateStyle) r.dateStyle = s.dateStyle;
      if (s.timeStyle) r.timeStyle = s.timeStyle;
      return r;
    }
    parts(ms) { return renderPattern(this, this.pattern, fieldsAt(this, ms)); }
    // like V8, format() swaps U+202F (ICU 72+, before AM/PM) for a plain space; formatToParts keeps it
    format(ms) { let out = ''; for (const p of this.parts(ms)) out += p.value; return out.indexOf('\u202F') < 0 ? out : out.replace(/\u202F/g, ' '); }
    rangeParts(a, b) {
      const s = this;
      const fa = fieldsAt(s, a), fb = fieldsAt(s, b);
      const pa = renderPattern(s, s.pattern, fa), pb = renderPattern(s, s.pattern, fb);
      const str = (ps) => ps.map((p) => p.value).join('');
      const tag = (ps, src) => ps.map((p) => ({ type: p.type, value: p.value, source: src }));
      if (!s.dateStyle && !s.timeStyle && !s.wider) {
        // like ICU, show the date (or year) when the ends fall on different days (years)
        const o = s.opts;
        const hasDate = o.year || o.month || o.day || o.weekday;
        let add = null;
        if (!hasDate && (fa.y !== fb.y || fa.M !== fb.M || fa.d !== fb.d)) add = { year: 'numeric', month: 'numeric', day: 'numeric' };
        else if (hasDate && !o.year && fa.y !== fb.y) add = { year: 'numeric' };
        if (add) {
          const w = Object.assign(Object.create(DateTimeFormatImpl.prototype), s, { opts: Object.assign({}, o, add), wider: true });
          w.pattern = componentPattern(w);
          return w.rangeParts(a, b);
        }
      }
      if (str(pa) === str(pb)) return tag(pa, 'shared');
      const sep = s.d.rs || ' – ';
      if (s.key.startsWith('en')) {
        const c = enCollapse(s, fa, fb, sep);
        if (c) return c;
      }
      return [...tag(pa, 'startRange'), { type: 'literal', value: sep, source: 'shared' }, ...tag(pb, 'endRange')];
    }
  }

  // English interval formats: the fields both ends share are shown once
  // ("Jan 5 – 7, 2026", "5 – 7 Jan 2026", "9:00 – 11:00 AM")
  function enCollapse(s, fa, fb, sep) {
    const toks = tokens(s.pattern);
    const tag = (ps, src) => ps.map((p) => ({ type: p.type, value: p.value, source: src }));
    const render = (from, to, f) => renderPattern(s, toks.slice(from, to).map((t) => (typeof t === 'string' ? t.replace(/[{}]/g, (c) => (c === '{' ? '{lb}' : '{rb}')) : '{' + t.f + t.w + '}')).join(''), f);
    const has = (fs) => toks.some((t) => typeof t !== 'string' && fs.includes(t.f));
    if (fa.y !== fb.y) return null;
    const hasTime = has('hms');
    const diff = new Set();
    if (fa.M !== fb.M || fa.d !== fb.d) {
      if (hasTime) return null;
      const mt = toks.find((t) => typeof t !== 'string' && (t.f === 'M' || t.f === 'L'));
      const text = mt && (/^[lsn]$/.test(mt.w) || (!mt.w && /^(long|short|narrow)$/.test(s.opts.month || '')));
      if (!text) return null;
      for (const f of 'dEc') diff.add(f);
      if (fa.M !== fb.M || has('Ec')) { diff.add('M'); diff.add('L'); }
    } else {
      if (!hasTime) return null;
      for (const f of 'hmsS') diff.add(f);
      if ((fa.h < 12) !== (fb.h < 12)) { diff.add('a'); diff.add('B'); }
    }
    let first = -1, last = -1;
    toks.forEach((t, i) => { if (typeof t !== 'string' && diff.has(t.f)) { if (first < 0) first = i; last = i; } });
    if (first < 0) return null;
    const pre = render(0, first, fa), a = render(first, last + 1, fa), b = render(first, last + 1, fb), post = render(last + 1, toks.length, fb);
    return [...tag(pre, 'shared'), ...tag(a, 'startRange'), { type: 'literal', value: sep, source: 'shared' }, ...tag(b, 'endRange'), ...tag(post, 'shared')];
  }

  function toDateValue(x) {
    let t;
    if (x === undefined) t = Date.now();
    else if (x instanceof Date) t = x.getTime();
    else if (typeof x === 'bigint') throw typeError('Cannot convert a BigInt value to a number');
    else t = Number(x);
    if (!Number.isFinite(t) || Math.abs(t) > 8.64e15) throw rangeError('Invalid time value');
    return Math.trunc(t) + 0;
  }

  // ---------- RelativeTimeFormat ----------
  const REL_UNITS = { second: 'second', seconds: 'second', minute: 'minute', minutes: 'minute', hour: 'hour', hours: 'hour', day: 'day', days: 'day', week: 'week', weeks: 'week', month: 'month', months: 'month', quarter: 'quarter', quarters: 'quarter', year: 'year', years: 'year' };
  class RelativeTimeFormatImpl {
    constructor(locales, options) {
      const o = coerceOptions(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.RelativeTimeFormat');
      let nu = getOpt(o, 'numberingSystem', 'string', null, undefined);
      if (nu !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(nu)) throw rangeError('Invalid numberingSystem : ' + nu);
      if (nu !== undefined && !hasOwn(NUMBERING, nu)) nu = undefined;
      const r = resolveLocale(locales, ['nu'], { nu });
      this.locale = r.locale; this.key = r.key; this.dataLocale = r.base;
      this.numberingSystem = r.ext.nu || 'latn';
      this.style = getOpt(o, 'style', 'string', ['long', 'short', 'narrow'], 'long', 'Intl.RelativeTimeFormat');
      this.numeric = getOpt(o, 'numeric', 'string', ['always', 'auto'], 'always', 'Intl.RelativeTimeFormat');
      this.d = LD(this.key);
      this.nf = new NumberFormatImpl(this.locale.replace(/-u-.*/, '') + (this.numberingSystem !== 'latn' ? '-u-nu-' + this.numberingSystem : ''), {});
    }
    resolved() { return { locale: this.locale, style: this.style, numeric: this.numeric, numberingSystem: this.numberingSystem }; }
    parts(value, unit) {
      value = Number(value);
      if (!Number.isFinite(value)) throw rangeError('Invalid value: ' + value);
      const u = REL_UNITS[String(unit)];
      if (!u) throw rangeError("Invalid unit argument for format() '" + String(unit) + "'");
      const rel = this.d.rel;
      const e = (this.style === 'narrow' && rel.n && rel.n[u]) || (this.style !== 'long' && rel.s[u]) || rel.l[u];
      if (this.numeric === 'auto' && Number.isInteger(value) && Math.abs(value) <= 2) {
        const k = Object.is(value, -0) ? '0' : String(value);
        if (e[2] && hasOwn(e[2], k)) return [{ type: 'literal', value: e[2][k] }];
      }
      const past = value < 0 || Object.is(value, -0);
      const np = this.nf.toParts(Math.abs(value));
      const numStr = np.parts.map((p) => p.value).join('');
      const pats = e[past ? 1 : 0];
      const pat = plv(this.d, pats, np.cat);
      void numStr;
      const i = pat.indexOf('{0}');
      const out = [];
      if (i > 0) out.push({ type: 'literal', value: pat.slice(0, i) });
      for (const p of np.parts) out.push({ type: p.type, value: p.value, unit: u });
      if (i + 3 < pat.length) out.push({ type: 'literal', value: pat.slice(i + 3) });
      return out;
    }
    format(v, u) { return this.parts(v, u).map((p) => p.value).join(''); }
  }

  // ---------- ListFormat ----------
  class ListFormatImpl {
    constructor(locales, options) {
      const o = optionsObject(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.ListFormat');
      const r = resolveLocale(locales, [], {});
      this.locale = r.locale; this.key = r.key; this.dataLocale = r.base;
      this.type = getOpt(o, 'type', 'string', ['conjunction', 'disjunction', 'unit'], 'conjunction', 'Intl.ListFormat');
      this.style = getOpt(o, 'style', 'string', ['long', 'short', 'narrow'], 'long', 'Intl.ListFormat');
      this.d = LD(this.key);
      this.pats = this.d.list[this.type[0] + this.style[0]];
    }
    resolved() { return { locale: this.locale, type: this.type, style: this.style }; }
    parts(list) {
      const xs = stringList(list);
      const n = xs.length;
      if (n === 0) return [];
      if (n === 1) return [{ type: 'element', value: xs[0] }];
      const [pair, start, middle, end] = this.pats;
      const lang = this.dataLocale.split('-')[0];
      const fix = (p, next) => {
        if (lang !== 'es') return p;
        // Spanish: "y" → "e" before an /i/ sound, "o" → "u" before an /o/ sound
        if (/ y /.test(p) && /^(i|hi)(?!a|e|o|u)/i.test(next)) return p.replace(' y ', ' e ');
        if (/ o /.test(p) && (/^(o|ho|8)/i.test(next) || /^11(\D|$)/.test(next))) return p.replace(' o ', ' u ');
        return p;
      };
      const apply = (p, a, bParts) => {
        const i0 = p.indexOf('{0}'), i1 = p.indexOf('{1}');
        const out = [];
        if (i0 > 0) out.push({ type: 'literal', value: p.slice(0, i0) });
        out.push({ type: 'element', value: a });
        const mid = p.slice(i0 + 3, i1);
        if (mid) out.push({ type: 'literal', value: mid });
        out.push(...bParts);
        if (i1 + 3 < p.length) out.push({ type: 'literal', value: p.slice(i1 + 3) });
        return out;
      };
      if (n === 2) return apply(fix(pair, xs[1]), xs[0], [{ type: 'element', value: xs[1] }]);
      let acc = apply(fix(end, xs[n - 1]), xs[n - 2], [{ type: 'element', value: xs[n - 1] }]);
      for (let i = n - 3; i >= 1; i--) acc = apply(middle, xs[i], acc);
      return apply(start, xs[0], acc);
    }
    format(list) { return this.parts(list).map((p) => p.value).join(''); }
  }
  function stringList(list) {
    if (list === undefined) return [];
    const out = [];
    for (const x of list) {
      if (typeof x !== 'string') throw typeError('Iterable yielded ' + (typeof x === 'object' ? (x === null ? 'null' : '[object Object]') : String(x)) + ' which is not a string');
      out.push(x);
    }
    return out;
  }

  // ---------- PluralRules ----------
  class PluralRulesImpl {
    constructor(locales, options) {
      const o = coerceOptions(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.PluralRules');
      this.type = getOpt(o, 'type', 'string', ['cardinal', 'ordinal'], 'cardinal', 'Intl.PluralRules');
      const r = resolveLocale(locales, [], {}, pluralKey);
      // ICU's plural data is per language: 'en-US' resolves to 'en'
      this.locale = r.isDefault ? r.locale : r.key; this.dataLocale = r.key;
      digitOptions(this, o, 0, 3, 'standard');
      this.fn = pluralFn(this.dataLocale);
    }
    resolved() {
      const s = this, r = { locale: s.locale, type: s.type, minimumIntegerDigits: s.minimumIntegerDigits };
      if (s.minimumFractionDigits !== undefined) { r.minimumFractionDigits = s.minimumFractionDigits; r.maximumFractionDigits = s.maximumFractionDigits; }
      if (s.minimumSignificantDigits !== undefined) { r.minimumSignificantDigits = s.minimumSignificantDigits; r.maximumSignificantDigits = s.maximumSignificantDigits; }
      r.pluralCategories = s.fn[s.type === 'ordinal' ? 3 : 2].split(' ');
      r.roundingIncrement = s.roundingIncrement; r.roundingMode = s.roundingMode; r.roundingPriority = s.roundingPriority; r.trailingZeroDisplay = s.trailingZeroDisplay;
      return r;
    }
    select(n) {
      n = Number(n);
      if (!Number.isFinite(n)) return 'other';
      const dec = toDecimal(n);
      const rr = applyRounding(this, dec);
      const [int, frac] = digitsOf(rr.x, this.minimumIntegerDigits, rr.minFrac);
      return this.fn[this.type === 'ordinal' ? 1 : 0](operands(int + (frac ? '.' + frac : '')));
    }
  }

  // ---------- Collator ----------
  // A UCA-like three-level comparison: primary = base letters (diacritics
  // stripped via NFD, compatibility forms folded via NFKD), secondary =
  // accents, tertiary = case and variant forms. Root order: whitespace <
  // punctuation < symbols < currency < digits < Latin < other scripts by
  // code point; a few common tailorings (sv, da/nb, es, pl, cs, tr, fi).
  const PUNCT_ORDER = "_-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$";
  const MARK_ORDER = '\u0301\u0300\u0306\u0302\u030C\u030A\u0308\u030B\u0303\u0307\u0338\u0327\u0328\u0304\u0331\u0323\u0324\u0325\u0326\u032D\u0330';
  const W_SPACE = 1e6, W_PUNCT = 2e6, W_SYM = 3e6, W_CUR = 4e6, W_DIGIT = 5e6, W_OWN = 6e6, W_LATIN = 8e6, W_OTHER = 9e6;
  // CLDR script reordering: the locale's own script sorts before Latin
  const OWN_SCRIPT = {
    ru: [[0x400, 0x52f]], uk: [[0x400, 0x52f]], bg: [[0x400, 0x52f]], be: [[0x400, 0x52f]], sr: [[0x400, 0x52f]], mk: [[0x400, 0x52f]], kk: [[0x400, 0x52f]], ky: [[0x400, 0x52f]], mn: [[0x400, 0x52f]],
    el: [[0x370, 0x3ff], [0x1f00, 0x1fff]], he: [[0x590, 0x5ff]], ar: [[0x600, 0x6ff]], fa: [[0x600, 0x6ff]], hi: [[0x900, 0x97f]], th: [[0xe00, 0xe7f]],
    ja: [[0x3040, 0x30ff], [0x3400, 0x4dbf], [0x4e00, 0x9fff], [0xff66, 0xff9f]], zh: [[0x3400, 0x4dbf], [0x4e00, 0x9fff]], ko: [[0x1100, 0x11ff], [0x3130, 0x318f], [0xac00, 0xd7af], [0x4e00, 0x9fff]],
  };
  const latinW = (c) => W_LATIN + (c.charCodeAt(0) - 97) * 16;
  const TAILOR = {
    sv: { 'å': ['z', 1], 'ä': ['z', 2], 'æ': ['z', 2, 1], 'ö': ['z', 3], 'ø': ['z', 3, 1] },
    fi: { 'å': ['z', 1], 'ä': ['z', 2], 'æ': ['z', 2, 1], 'ö': ['z', 3], 'ø': ['z', 3, 1] },
    da: { 'æ': ['z', 1], 'ä': ['z', 1, 1], 'ø': ['z', 2], 'ö': ['z', 2, 1], 'å': ['z', 3] },
    nb: { 'æ': ['z', 1], 'ä': ['z', 1, 1], 'ø': ['z', 2], 'ö': ['z', 2, 1], 'å': ['z', 3] },
    es: { 'ñ': ['n', 1] },
    pl: { 'ą': ['a', 1], 'ć': ['c', 1], 'ę': ['e', 1], 'ł': ['l', 1], 'ń': ['n', 1], 'ó': ['o', 1], 'ś': ['s', 1], 'ź': ['z', 1], 'ż': ['z', 2] },
    cs: { 'č': ['c', 1], 'ř': ['r', 1], 'š': ['s', 1], 'ž': ['z', 1] },
    tr: { 'ç': ['c', 1], 'ğ': ['g', 1], 'ı': ['h', 15], 'ö': ['o', 1], 'ş': ['s', 1], 'ü': ['u', 1] },
  };
  TAILOR.nn = TAILOR.no = TAILOR.nb; TAILOR.sk = TAILOR.cs; TAILOR.az = TAILOR.tr;
  // letters without a decomposition: [base, secondary mark rank, extra]
  const LATIN_EXTRA = { 'ß': 'ss', 'æ': 'ae', 'œ': 'oe', 'ĳ': 'ij', 'ø': ['o', 40], 'đ': ['d', 40], 'ð': ['d', 41], 'ł': ['l', 40], 'ħ': ['h', 40], 'ŧ': ['t', 40], 'ı': ['i', 0, 1], 'ŀ': ['l', 43], 'þ': ['z', 0, 4], 'ə': ['e', 0, 2], 'ɛ': ['e', 0, 3], 'ŋ': ['n', 0, 2], 'ɔ': ['o', 0, 2], 'ƒ': ['f', 40], 'ſ': ['s', 44] };
  let RE_MARK, RE_LETTER, RE_DIGIT, RE_SPACE, RE_PUNCT, RE_CUR, RE_SYM;
  function collRegexes() {
    if (RE_MARK) return;
    RE_MARK = U(String.raw`^\p{M}$`); RE_LETTER = U(String.raw`^\p{L}$`); RE_DIGIT = U(String.raw`^\p{Nd}$`); RE_SPACE = U(String.raw`^[\p{Z}\s\p{Cc}]$`);
    RE_PUNCT = U(String.raw`^\p{P}$`); RE_CUR = U(String.raw`^\p{Sc}$`); RE_SYM = U(String.raw`^[\p{S}\p{No}\p{Nl}]$`);
  }
  const DIGIT_ZEROS = [0x30, 0x660, 0x6f0, 0x7c0, 0x966, 0x9e6, 0xa66, 0xae6, 0xb66, 0xbe6, 0xc66, 0xce6, 0xd66, 0xde6, 0xe50, 0xed0, 0xf20, 0x1040, 0x1090, 0x17e0, 0x1810, 0xff10];
  function digitValue(cp) {
    for (const z of DIGIT_ZEROS) if (cp >= z && cp <= z + 9) return cp - z;
    return cp & 0xf;
  }
  const SMALL_KANA = '\u3041\u3043\u3045\u3047\u3049\u3063\u3083\u3085\u3087\u308E\u3095\u3096';

  class CollatorImpl {
    constructor(locales, options) {
      const o = coerceOptions(options);
      this.usage = getOpt(o, 'usage', 'string', ['sort', 'search'], 'sort', 'Intl.Collator');
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.Collator');
      let co = getOpt(o, 'collation', 'string', null, undefined);
      if (co !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(co)) throw rangeError('Invalid collation : ' + co);
      if (co !== undefined) co = EXT_OK.co(co) || undefined;
      const numeric = getOpt(o, 'numeric', 'boolean', null, undefined);
      const caseFirst = getOpt(o, 'caseFirst', 'string', ['upper', 'lower', 'false'], undefined, 'Intl.Collator');
      const r = resolveLocale(locales, ['co', 'kn', 'kf'], { co, kn: numeric === undefined ? undefined : String(numeric), kf: caseFirst }, anyLang);
      this.locale = r.locale; this.dataLocale = r.base;
      this.collation = r.ext.co || 'default';
      this.numeric = r.ext.kn === 'true' || r.ext.kn === '';
      this.caseFirst = r.ext.kf || 'false';
      let sens = getOpt(o, 'sensitivity', 'string', ['base', 'accent', 'case', 'variant'], undefined, 'Intl.Collator');
      if (sens === undefined) sens = 'variant';
      this.sensitivity = sens;
      const lang = this.dataLocale.split('-')[0];
      this.ignorePunctuation = getOpt(o, 'ignorePunctuation', 'boolean', null, lang === 'th');
      this.tailor = TAILOR[lang] || null;
      this.dotless = lang === 'tr' || lang === 'az';
      this.own = OWN_SCRIPT[lang] || null;
      this.ownAfterLatin = lang === 'ja' || lang === 'zh' || lang === 'ko';
      this.kataFirst = lang === 'ja';
      this.upperDefault = lang === 'da' && !r.ext.kf; // CLDR: Danish sorts uppercase first
      this.cache = new Map();
      collRegexes();
    }
    resolved() {
      return { locale: this.locale, usage: this.usage, sensitivity: this.sensitivity, ignorePunctuation: this.ignorePunctuation, collation: this.collation, numeric: this.numeric, caseFirst: this.caseFirst };
    }
    key(str) {
      let k = this.cache.get(str);
      if (k) return k;
      const P = [], S = [], T = [], C = [];
      const upperFirst = this.caseFirst === 'upper' || (this.caseFirst === 'false' && this.upperDefault);
      const push = (w, sec, ter, upper) => {
        P.push(w);
        S.push(sec);
        const cs = upper ? (upperFirst ? 0 : 1) : (upperFirst ? 1 : 0);
        C.push(cs);
        T.push(cs * 16 + ter);
      };
      const tailor = this.tailor;
      const cps = Array.from(str);
      for (let i = 0; i < cps.length; i++) {
        let orig = cps[i];
        let lower = this.dotless ? (orig === 'I' ? '\u0131' : orig === '\u0130' ? 'i' : orig.toLowerCase()) : orig.toLowerCase();
        const upper = orig !== lower && lower.length === 1;
        if (this.dotless && orig === '\u0130') { push(latinW('i'), '\u0001', 0, true); continue; }
        if (tailor && hasOwn(tailor, lower)) {
          const [b, off, sec] = tailor[lower];
          push(latinW(b) + off, sec ? '\u0001\u0060' : '\u0001', 0, upper);
          continue;
        }
        const nfd = orig.normalize('NFD');
        let ter = 0;
        let chars = Array.from(nfd);
        const kd = orig.normalize('NFKD');
        if (kd !== nfd) { chars = Array.from(kd); ter = 2; }
        for (const c of chars) {
          const cp = c.codePointAt(0);
          if (RE_MARK.test(c)) {
            const r = MARK_ORDER.indexOf(c);
            if (S.length) S[S.length - 1] = S[S.length - 1] + String.fromCharCode(2 + (r < 0 ? 30 + (cp & 0xff) : r));
            else push(0, String.fromCharCode(2 + (r < 0 ? 30 : r)), 0, false);
            continue;
          }
          const lc = c.toLowerCase();
          const up = c !== lc || (upper && chars.length === 1);
          if (lc >= 'a' && lc <= 'z' && lc.length === 1) { push(latinW(lc), '\u0001', ter, up); continue; }
          const ex = LATIN_EXTRA[lc];
          if (ex) {
            if (typeof ex === 'string') { let first = true; for (const x of ex) { push(latinW(x), first && lc === 'ß' ? '\u0001\u0060' : '\u0001', ter + 1, up); first = false; } }
            else push(latinW(ex[0]) + (ex[2] || 0), ex[1] ? '\u0001' + String.fromCharCode(ex[1]) : '\u0001', ter, up);
            continue;
          }
          if (RE_DIGIT.test(c)) {
            if (this.numeric) {
              let j = i, run = '';
              // collect the digit run from the original string
              while (j < cps.length && RE_DIGIT.test(cps[j])) run += String(digitValue(cps[j++].codePointAt(0)));
              i = j - 1;
              run = run.replace(/^0+(?=\d)/, '');
              push(W_DIGIT + 100 + run.length, '\u0001', 0, false);
              for (const dch of run) push(W_DIGIT + +dch, '\u0001', 0, false);
              break;
            }
            push(W_DIGIT + digitValue(cp), '\u0001', ter, false);
            continue;
          }
          if (RE_LETTER.test(c)) {
            let w = lc.codePointAt(0), t = ter;
            if (w >= 0x30a1 && w <= 0x30f6) { w -= 0x60; t += this.kataFirst ? 0 : 1; }
            else if (this.kataFirst && w >= 0x3041 && w <= 0x3096) t += 1;
            const si = SMALL_KANA.indexOf(String.fromCodePoint(w));
            if (si >= 0) { w += 1; t += 1; }
            const own = this.own && this.own.some((r) => w >= r[0] && w <= r[1]);
            push((own ? (this.ownAfterLatin ? W_LATIN + 1e5 : W_OWN) : W_OTHER) + w, '\u0001', t, up);
            continue;
          }
          if (RE_SPACE.test(c)) { if (!this.ignorePunctuation) push(W_SPACE + cp, '\u0001', ter, false); continue; }
          if (RE_PUNCT.test(c)) {
            if (this.ignorePunctuation) continue;
            const r = PUNCT_ORDER.indexOf(c);
            push(W_PUNCT + (r >= 0 ? r : 100 + cp), '\u0001', ter, false);
            continue;
          }
          if (RE_CUR.test(c)) { push(W_CUR + cp, '\u0001', ter, false); continue; }
          if (c === '$') { push(W_CUR + cp, '\u0001', ter, false); continue; }
          if (RE_SYM.test(c)) { const r = PUNCT_ORDER.indexOf(c); push(W_SYM + (r >= 0 ? r : 100 + cp), '\u0001', ter, false); continue; }
          push(W_OTHER + cp, '\u0001', ter, false); // unassigned / other
        }
      }
      const enc = (a) => { let s = ''; for (const w of a) s += String.fromCharCode(w >>> 16, w & 0xffff); return s; };
      k = [enc(P), S.join('\u0000'), String.fromCharCode(...T), String.fromCharCode(...C)];
      if (this.cache.size > 5000) this.cache.clear();
      this.cache.set(str, k);
      return k;
    }
    compare(a, b) {
      a = String(a); b = String(b);
      if (a === b) return 0;
      const ka = this.key(a), kb = this.key(b);
      const cmp = (x, y) => (x < y ? -1 : x > y ? 1 : 0);
      let r = cmp(ka[0], kb[0]);
      if (r || this.sensitivity === 'base') return r;
      if (this.sensitivity === 'case') return cmp(ka[3], kb[3]);
      r = cmp(ka[1], kb[1]);
      if (r || this.sensitivity === 'accent') return r;
      return cmp(ka[2], kb[2]);
    }
  }

  // ---------- Segmenter ----------
  // Grapheme clusters per UAX #29 (incl. GB9c Indic conjuncts and GB11 emoji
  // ZWJ sequences); words: UAX #29 word rules over grapheme clusters, CJK
  // ideographs one per word; sentences: UAX #29 sentence rules, simplified.
  const GC = { Other: 0, CR: 1, LF: 2, Control: 3, Extend: 4, ZWJ: 5, RI: 6, Prepend: 7, SpacingMark: 8, L: 9, V: 10, T: 11, LV: 12, LVT: 13 };
  let gRe;
  function graphemeRegexes() {
    if (gRe) return gRe;
    const re = (s, fb) => { try { return new RegExp(s, 'u'); } catch { return new RegExp(fb, 'u'); } };
    gRe = {
      control: re('^[\\p{Cc}\\p{Zl}\\p{Zp}\\p{Cs}\\u00AD\\u061C\\u180E\\u200B\\u200E\\u200F\\u2028-\\u202E\\u2060-\\u206F\\uFEFF\\uFFF0-\\uFFFB\\u{E0000}-\\u{E001F}\\u{E0080}-\\u{E0FFF}\\u{13430}-\\u{1343F}\\u{1BCA0}-\\u{1BCA3}\\u{1D173}-\\u{1D17A}]$', '^[\\p{Cc}\\p{Zl}\\p{Zp}]$'),
      extend: re('^[\\p{Grapheme_Extend}\\p{Emoji_Modifier}\\u200C]$', '^[\\p{Mn}\\p{Me}\\u200C\\u{1F3FB}-\\u{1F3FF}]$'),
      spacing: re('^\\p{Mc}$', '^\\p{Mc}$'),
      pict: re('^\\p{Extended_Pictographic}$', '^[\\u{1F000}-\\u{1FAFF}\\u2600-\\u27BF]$'),
      prepend: /^[\u0600-\u0605\u06DD\u070F\u0890\u0891\u08E2\u0D4E\u{110BD}\u{110CD}\u{111C2}\u{111C3}\u{1193F}\u{11941}\u{11A3A}\u{11A84}-\u{11A89}\u{11D46}\u{11F02}]$/u,
      // Indic conjunct break (GB9c): consonants and virama linkers
      incbCons: /^[\u0915-\u0939\u0958-\u095F\u0978-\u097F\u0995-\u09A8\u09AA-\u09B0\u09B2\u09B6-\u09B9\u09DC\u09DD\u09DF\u09F0\u09F1\u0A95-\u0AA8\u0AAA-\u0AB0\u0AB2\u0AB3\u0AB5-\u0AB9\u0AF9\u0B15-\u0B28\u0B2A-\u0B30\u0B32\u0B33\u0B35-\u0B39\u0B5C\u0B5D\u0B5F\u0B71\u0C15-\u0C28\u0C2A-\u0C39\u0C58-\u0C5A\u0D15-\u0D3A]$/u,
      incbLink: /^[\u094D\u09CD\u0ACD\u0B4D\u0C4D\u0D4D]$/u,
    };
    return gRe;
  }
  const gcCache = new Map();
  function gcat(cp) {
    if (cp < 0x7f) return cp === 13 ? GC.CR : cp === 10 ? GC.LF : cp < 0x20 || cp === 0x7f ? GC.Control : GC.Other;
    let c = gcCache.get(cp);
    if (c !== undefined) return c;
    const r = graphemeRegexes();
    const ch = String.fromCodePoint(cp);
    if (cp === 0x200d) c = GC.ZWJ;
    else if (cp >= 0x1f1e6 && cp <= 0x1f1ff) c = GC.RI;
    else if (cp >= 0xac00 && cp <= 0xd7a3) c = (cp - 0xac00) % 28 === 0 ? GC.LV : GC.LVT;
    else if ((cp >= 0x1100 && cp <= 0x115f) || (cp >= 0xa960 && cp <= 0xa97c)) c = GC.L;
    else if ((cp >= 0x1160 && cp <= 0x11a7) || (cp >= 0xd7b0 && cp <= 0xd7c6)) c = GC.V;
    else if ((cp >= 0x11a8 && cp <= 0x11ff) || (cp >= 0xd7cb && cp <= 0xd7fb)) c = GC.T;
    else if (r.extend.test(ch)) c = GC.Extend;
    else if (r.prepend.test(ch)) c = GC.Prepend;
    else if (r.control.test(ch)) c = GC.Control;
    else if (r.spacing.test(ch) || cp === 0x0e33 || cp === 0x0eb3) c = GC.SpacingMark;
    else c = GC.Other;
    if (gcCache.size > 8000) gcCache.clear();
    gcCache.set(cp, c);
    return c;
  }
  const isPict = (cp) => cp >= 0xa9 && graphemeRegexes().pict.test(String.fromCodePoint(cp));

  // grapheme boundaries (UTF-16 indices, including 0 and length)
  function graphemeBreaks(s) {
    const out = [0];
    const n = s.length;
    if (!n) return out;
    let i = 0;
    let cp = s.codePointAt(0), prev = gcat(cp);
    let ri = prev === GC.RI ? 1 : 0;
    // emoji state: saw ExtPict (Extend)* and then ZWJ
    let pict = isPict(cp), pictZwj = false;
    const R = graphemeRegexes();
    let incb = R.incbCons.test(String.fromCodePoint(cp)) ? 1 : 0; // 1: consonant seen, 2: consonant+linker seen
    i += cp > 0xffff ? 2 : 1;
    while (i < n) {
      cp = s.codePointAt(i);
      const cur = gcat(cp);
      const ch = String.fromCodePoint(cp);
      let brk;
      if (prev === GC.CR && cur === GC.LF) brk = false;
      else if (prev === GC.CR || prev === GC.LF || prev === GC.Control) brk = true;
      else if (cur === GC.CR || cur === GC.LF || cur === GC.Control) brk = true;
      else if (prev === GC.L && (cur === GC.L || cur === GC.V || cur === GC.LV || cur === GC.LVT)) brk = false;
      else if ((prev === GC.LV || prev === GC.V) && (cur === GC.V || cur === GC.T)) brk = false;
      else if ((prev === GC.LVT || prev === GC.T) && cur === GC.T) brk = false;
      else if (cur === GC.Extend || cur === GC.ZWJ || cur === GC.SpacingMark) brk = false;
      else if (prev === GC.Prepend) brk = false;
      else if (incb === 2 && R.incbCons.test(ch)) brk = false;
      else if (prev === GC.ZWJ && pictZwj && isPict(cp)) brk = false;
      else if (prev === GC.RI && cur === GC.RI) brk = ri % 2 === 0;
      else brk = true;
      if (brk) out.push(i);
      // update states
      if (cur === GC.RI) ri = brk || prev !== GC.RI ? 1 : ri + 1;
      else ri = 0;
      const pc = isPict(cp);
      if (pc) { pict = true; pictZwj = false; }
      else if (cur === GC.Extend && pict) { /* stay */ }
      else if (cur === GC.ZWJ && pict) { pictZwj = true; pict = false; }
      else { pict = false; pictZwj = false; }
      if (R.incbCons.test(ch)) incb = 1;
      else if (R.incbLink.test(ch) && incb) incb = 2;
      else if (!(cur === GC.Extend || cur === GC.ZWJ) || !incb) incb = 0;
      prev = cur;
      i += cp > 0xffff ? 2 : 1;
    }
    out.push(n);
    return out;
  }

  // word classes per cluster (by its first code point)
  const WC = { Other: 0, Letter: 1, Numeric: 2, Katakana: 3, Ideo: 4, Hira: 5, MidLetter: 6, MidNum: 7, MidNumLet: 8, ExtendNumLet: 9, Space: 10, Newline: 11, RI: 12, Pict: 13, Hebrew: 14, SQ: 15, DQ: 16 };
  let wRe;
  function wordClass(seg) {
    // a cluster is classed by its first code point after any prepended format characters
    let k = 0;
    while (k < seg.length - 1 && /^[\u0600-\u0605\u06DD\u070F\u0890\u0891\u08E2\u200B-\u200F\uFEFF]$/.test(seg[k])) k++;
    if (k) seg = seg.slice(k);
    const cp = seg.codePointAt(0);
    if (!wRe) wRe = { letter: U(String.raw`^[\p{L}\p{Nl}\p{Mn}\p{Mc}]`), num: U(String.raw`^\p{Nd}`), kata: U(String.raw`^[\p{Script=Katakana}\u3031-\u3035\u309B\u309C\u30A0\u30FC\uFF70]`), ideo: U(String.raw`^[\p{Script=Han}\p{Ideographic}]`), hira: U(String.raw`^\p{Script=Hiragana}`), space: U(String.raw`^[\p{Zs}\t]`), hebrew: U(String.raw`^\p{Script=Hebrew}`), thai: U(String.raw`^[\p{Script=Thai}\p{Script=Lao}\p{Script=Khmer}\p{Script=Myanmar}]`) };
    if (cp === 10 || cp === 13 || cp === 0x0b || cp === 0x0c || cp === 0x85 || cp === 0x2028 || cp === 0x2029) return WC.Newline;
    if (cp === 0x27) return WC.SQ;
    if (cp === 0x22) return WC.DQ;
    if (cp === 0x2e || cp === 0x2018 || cp === 0x2019 || cp === 0x2024 || cp === 0xfe52 || cp === 0xff07 || cp === 0xff0e) return WC.MidNumLet;
    if (cp === 0x3a || cp === 0xb7 || cp === 0x387 || cp === 0x55f || cp === 0x5f4 || cp === 0x2027 || cp === 0xfe13 || cp === 0xfe55 || cp === 0xff1a) return WC.MidLetter;
    if (cp === 0x2c || cp === 0x3b || cp === 0x37e || cp === 0x589 || cp === 0x60c || cp === 0x60d || cp === 0x66c || cp === 0x7f8 || cp === 0x2044 || cp === 0xfe10 || cp === 0xfe14 || cp === 0xfe50 || cp === 0xfe54 || cp === 0xff0c || cp === 0xff1b) return WC.MidNum;
    if (cp === 0x5f || cp === 0x202f || cp === 0x203f || cp === 0x2040 || cp === 0x2054 || cp === 0xfe33 || cp === 0xfe34 || cp === 0xfe4d || cp === 0xfe4e || cp === 0xfe4f || cp === 0xff3f) return WC.ExtendNumLet;
    if (cp >= 0x1f1e6 && cp <= 0x1f1ff) return WC.RI;
    if (wRe.space.test(seg)) return WC.Space;
    if (wRe.kata.test(seg)) return WC.Katakana;
    if (wRe.hira.test(seg)) return WC.Hira;
    if (wRe.ideo.test(seg)) return WC.Ideo;
    if (wRe.num.test(seg)) return WC.Numeric;
    if (wRe.hebrew.test(seg) && wRe.letter.test(seg)) return WC.Hebrew;
    if (wRe.letter.test(seg)) return WC.Letter;
    if (isPict(cp)) return WC.Pict;
    return WC.Other;
  }
  const isAH = (c) => c === WC.Letter || c === WC.Hebrew;
  // → [[start, end, isWordLike], ...]
  function wordSegments(s) {
    const gb = graphemeBreaks(s);
    const segs = [], cls = [];
    for (let i = 0; i + 1 < gb.length; i++) { segs.push([gb[i], gb[i + 1]]); cls.push(wordClass(s.slice(gb[i], gb[i + 1]))); }
    const out = [];
    let start = 0;
    const n = segs.length;
    for (let i = 1; i <= n; i++) {
      let brk = true;
      if (i < n) {
        const a = cls[i - 1], b = cls[i];
        const a2 = i >= 2 ? cls[i - 2] : -1, b2 = i + 1 < n ? cls[i + 1] : -1;
        if (a === WC.Newline || b === WC.Newline) brk = true;
        else if (a === WC.Space && b === WC.Space) brk = false;
        else if (isAH(a) && isAH(b)) brk = false;
        else if (isAH(a) && (b === WC.MidLetter || b === WC.MidNumLet || b === WC.SQ) && isAH(b2)) brk = false;
        else if (isAH(a2) && (a === WC.MidLetter || a === WC.MidNumLet || a === WC.SQ) && isAH(b)) brk = false;
        else if (a === WC.Hebrew && b === WC.SQ) brk = false;
        else if (a === WC.Hebrew && b === WC.DQ && b2 === WC.Hebrew) brk = false;
        else if (a2 === WC.Hebrew && a === WC.DQ && b === WC.Hebrew) brk = false;
        else if (a === WC.Numeric && b === WC.Numeric) brk = false;
        else if (isAH(a) && b === WC.Numeric) brk = false;
        else if (a === WC.Numeric && isAH(b)) brk = false;
        else if (a2 === WC.Numeric && (a === WC.MidNum || a === WC.MidNumLet || a === WC.SQ) && b === WC.Numeric) brk = false;
        else if (a === WC.Numeric && (b === WC.MidNum || b === WC.MidNumLet || b === WC.SQ) && b2 === WC.Numeric) brk = false;
        else if (a === WC.Katakana && b === WC.Katakana) brk = false;
        else if ((isAH(a) || a === WC.Numeric || a === WC.Katakana || a === WC.ExtendNumLet) && b === WC.ExtendNumLet) brk = false;
        else if (a === WC.ExtendNumLet && (isAH(b) || b === WC.Numeric || b === WC.Katakana)) brk = false;
        // regional indicator pairs are already single clusters
      }
      if (brk) {
        const a = segs[start][0], b = segs[i - 1][1];
        let like = false;
        for (let j = start; j < i; j++) { const c = cls[j]; if (c === WC.Letter || c === WC.Hebrew || c === WC.Numeric || c === WC.Katakana || c === WC.Ideo || c === WC.Hira) { like = true; break; } }
        out.push([a, b, like]);
        start = i;
      }
    }
    return out;
  }

  // sentences: break after . ! ? (and CJK/fullwidth forms) + closing punctuation + spaces,
  // unless a lowercase letter or a digit follows the period
  let sRe;
  function sentenceSegments(s) {
    if (!sRe) sRe = { term: /[!?\u0589\u061F\u06D4\u0700-\u0702\u07F9\u0964\u0965\u104A\u104B\u1362\u1367\u1368\u166E\u1803\u1809\u1944\u1945\u203C\u203D\u2047-\u2049\u2E2E\u3002\uFE56\uFE57\uFF01\uFF1F\uFF61]/u, aterm: /[.\u2024\uFE52\uFF0E]/u, close: U(String.raw`[\p{Ps}\p{Pe}\p{Pi}\p{Pf}"'«»]`), sp: U(String.raw`[\p{Zs}\t]`), sep: /[\n\r\u0085\u2028\u2029]/u, lower: U(String.raw`\p{Ll}`), upper: U(String.raw`[\p{Lu}\p{Lt}]`), letter: U(String.raw`\p{L}`), digit: U(String.raw`\p{Nd}`) };
    const out = [];
    const n = s.length;
    let start = 0, i = 0;
    const cpAt = (k) => String.fromCodePoint(s.codePointAt(k));
    const step = (k) => (s.codePointAt(k) > 0xffff ? 2 : 1);
    while (i < n) {
      const c = cpAt(i);
      if (sRe.sep.test(c)) {
        let j = i + step(i);
        if (c === '\r' && s[j] === '\n') j++;
        out.push([start, j]); start = i = j; continue;
      }
      const isT = sRe.term.test(c), isA = sRe.aterm.test(c);
      if (!isT && !isA) { i += step(i); continue; }
      let j = i + step(i);
      // more terminators
      while (j < n && (sRe.term.test(cpAt(j)) || sRe.aterm.test(cpAt(j)))) j += step(j);
      if (isA && j === i + 1) {
        const nx = j < n ? cpAt(j) : '';
        // "3.14", "U.S.A" \u2014 no break
        if (nx && (sRe.digit.test(nx) || (sRe.upper.test(nx) && i > 0 && sRe.upper.test(cpAt(i - 1))))) { i = j; continue; }
      }
      while (j < n && sRe.close.test(cpAt(j))) j += step(j);
      const spStart = j;
      while (j < n && sRe.sp.test(cpAt(j))) j += step(j);
      if (j < n && sRe.sep.test(cpAt(j))) {
        let k = j + step(j);
        if (cpAt(j) === '\r' && s[k] === '\n') k++;
        out.push([start, k]); start = i = k; continue;
      }
      if (j >= n) { i = j; break; }
      if (isA && !isT) {
        // a lowercase continuation means it was an abbreviation
        let k = j;
        while (k < n && !sRe.letter.test(cpAt(k)) && !sRe.term.test(cpAt(k)) && !sRe.aterm.test(cpAt(k)) && !sRe.sep.test(cpAt(k))) k += step(k);
        if (k < n && sRe.lower.test(cpAt(k))) { i = j; continue; }
        if (spStart === j && !sRe.upper.test(cpAt(j))) { i = j; continue; }
      }
      out.push([start, j]); start = i = j;
    }
    if (start < n) out.push([start, n]);
    return out;
  }

  class SegmenterImpl {
    constructor(locales, options) {
      const o = optionsObject(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.Segmenter');
      const r = resolveLocale(locales, [], {}, anyLang);
      this.locale = r.locale;
      this.granularity = getOpt(o, 'granularity', 'string', ['grapheme', 'word', 'sentence'], 'grapheme', 'Intl.Segmenter');
    }
    resolved() { return { locale: this.locale, granularity: this.granularity }; }
    // [[start, end, isWordLike?], ...]
    segmentsOf(str) {
      if (this.granularity === 'word') return wordSegments(str);
      if (this.granularity === 'sentence') return sentenceSegments(str);
      const b = graphemeBreaks(str), out = [];
      for (let i = 0; i + 1 < b.length; i++) out.push([b[i], b[i + 1]]);
      return out;
    }
  }

  // ---------- DisplayNames ----------
  const DN_FIELDS = ['era', 'year', 'quarter', 'month', 'weekOfYear', 'weekday', 'day', 'dayPeriod', 'hour', 'minute', 'second', 'timeZoneName'];
  class DisplayNamesImpl {
    constructor(locales, options) {
      if (options === undefined) throw typeError("Required option 'type' is missing");
      const o = optionsObject(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.DisplayNames');
      const r = resolveLocale(locales, [], {});
      this.locale = r.locale; this.key = r.key;
      this.style = getOpt(o, 'style', 'string', ['narrow', 'short', 'long'], 'long', 'Intl.DisplayNames');
      const type = getOpt(o, 'type', 'string', ['language', 'region', 'script', 'currency', 'calendar', 'dateTimeField'], undefined, 'Intl.DisplayNames');
      if (type === undefined) throw typeError("Required option 'type' is missing");
      this.type = type;
      this.fallback = getOpt(o, 'fallback', 'string', ['code', 'none'], 'code', 'Intl.DisplayNames');
      const ld = getOpt(o, 'languageDisplay', 'string', ['dialect', 'standard'], 'dialect', 'Intl.DisplayNames');
      if (type === 'language') this.languageDisplay = ld;
      this.d = LD(this.key).dn || {};
      this.en = LD('en').dn;
    }
    resolved() {
      const r = { locale: this.locale, style: this.style, type: this.type, fallback: this.fallback };
      if (this.type === 'language') r.languageDisplay = this.languageDisplay;
      return r;
    }
    table(name) {
      // localized table, else English
      const own = this.d[name];
      return (k) => {
        if (own && hasOwn(own, k)) return own[k];
        if (this.d === this.en || !own || !this.d.lang) return hasOwn(this.en[name] || {}, k) ? this.en[name][k] : undefined;
        return hasOwn(this.en[name] || {}, k) ? this.en[name][k] : undefined;
      };
    }
    name(kind, code, own) {
      const short = this.style !== 'long';
      const get = (k) => (own ? (this.d[k] && hasOwn(this.d[k], code) ? this.d[k][code] : this.d === this.en || this.d[kind] ? undefined : this.table(k)(code)) : this.table(k)(code));
      if (short) { const v = get(kind + 'S'); if (v !== undefined) return v; }
      return get(kind);
    }
    of(code) {
      if (code === undefined) throw typeError('invalid_argument');
      code = String(code);
      const t = this.type;
      const fb = (c) => (this.fallback === 'code' ? c : undefined);
      switch (t) {
        case 'region': {
          if (!/^([A-Za-z]{2}|\d{3})$/.test(code)) throw rangeError('invalid_argument');
          const c = code.toUpperCase();
          const v = this.name('reg', c);
          return v !== undefined ? v : fb(c);
        }
        case 'script': {
          if (!/^[A-Za-z]{4}$/.test(code)) throw rangeError('invalid_argument');
          const c = code[0].toUpperCase() + code.slice(1).toLowerCase();
          const v = this.table('scr')(c);
          return v !== undefined ? v : fb(c);
        }
        case 'currency': {
          if (!/^[A-Za-z]{3}$/.test(code)) throw rangeError('invalid_argument');
          const c = code.toUpperCase();
          const v = this.table('cur')(c);
          return v !== undefined ? v : fb(c);
        }
        case 'calendar': {
          if (!/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(code)) throw rangeError('invalid_argument');
          const c = code.toLowerCase() === 'gregorian' ? 'gregory' : code.toLowerCase();
          const v = this.table('cal')(c);
          return v !== undefined ? v : fb(c);
        }
        case 'dateTimeField': {
          const i = DN_FIELDS.indexOf(code);
          if (i < 0) throw rangeError('invalid_argument');
          const f = this.d.fld || this.en.fld;
          return (f[this.style[0]] || f.s || f.l)[i];
        }
        default: {
          const r = parseTag(code);
          if (!r || r.ext.length || r.priv || code.includes('_')) throw rangeError('invalid_argument');
          const tagStr = canonTag(code);
          const p = parseTag(tagStr);
          const base = p.lang;
          if (this.languageDisplay === 'dialect') {
            for (const k of [tagStr, base + (p.script ? '-' + p.script : '') + (p.region ? '-' + p.region : ''), p.script ? base + '-' + p.script : null, p.region && !p.script ? base + '-' + p.region : null]) {
              if (!k || k === base) continue;
              const v = this.name('lang', k, true);
              if (v === undefined) continue;
              // remaining subtags in parentheses
              const rest = [];
              if (p.script && !k.includes('-' + p.script)) rest.push(this.table('scr')(p.script) || p.script);
              if (p.region && !k.includes('-' + p.region)) rest.push(this.name('reg', p.region) || p.region);
              for (const vv of p.variants) rest.push(vv.toUpperCase());
              return rest.length ? this.pattern(v, rest) : v;
            }
          }
          const ln = this.name('lang', base);
          if (ln === undefined) return this.fallback === 'code' ? tagStr : undefined;
          const rest = [];
          if (p.script) rest.push(this.table('scr')(p.script) || p.script);
          if (p.region) rest.push(this.name('reg', p.region) || p.region);
          for (const vv of p.variants) rest.push(vv.toUpperCase());
          return rest.length ? this.pattern(ln, rest) : ln;
        }
      }
    }
    pattern(name, rest) {
      const lp = this.d.lp || this.en.lp;
      return lp[0].replace('{0}', name).replace('{1}', rest.join(lp[1]));
    }
  }

  // ---------- DurationFormat ----------
  const DUR_UNITS = ['years', 'months', 'weeks', 'days', 'hours', 'minutes', 'seconds', 'milliseconds', 'microseconds', 'nanoseconds'];
  const DUR_SINGULAR = { years: 'year', months: 'month', weeks: 'week', days: 'day', hours: 'hour', minutes: 'minute', seconds: 'second', milliseconds: 'millisecond', microseconds: 'microsecond', nanoseconds: 'nanosecond' };
  class DurationFormatImpl {
    constructor(locales, options) {
      const o = optionsObject(options);
      getOpt(o, 'localeMatcher', 'string', ['lookup', 'best fit'], 'best fit', 'Intl.DurationFormat');
      let nu = getOpt(o, 'numberingSystem', 'string', null, undefined);
      if (nu !== undefined && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(nu)) throw rangeError('Invalid numberingSystem : ' + nu);
      if (nu !== undefined && !hasOwn(NUMBERING, nu)) nu = undefined;
      const r = resolveLocale(locales, ['nu'], { nu });
      this.locale = r.locale; this.key = r.key;
      this.numberingSystem = r.ext.nu || 'latn';
      this.style = getOpt(o, 'style', 'string', ['long', 'short', 'narrow', 'digital'], 'short', 'Intl.DurationFormat');
      this.u = {};
      let prev = null;
      for (const u of DUR_UNITS) {
        const digital = this.style === 'digital';
        const timeish = ['hours', 'minutes', 'seconds'].includes(u), sub = ['milliseconds', 'microseconds', 'nanoseconds'].includes(u);
        const vals = timeish ? ['long', 'short', 'narrow', 'numeric', '2-digit'] : sub ? ['long', 'short', 'narrow', 'numeric'] : ['long', 'short', 'narrow'];
        let st = getOpt(o, u, 'string', vals, undefined, 'Intl.DurationFormat');
        let dispDefault = 'always';
        if (st === undefined) {
          if (digital && (timeish || sub)) st = u === 'hours' ? 'numeric' : sub ? 'numeric' : 'numeric';
          else st = digital ? 'short' : this.style;
          if (prev === 'numeric' || prev === '2-digit') st = timeish && u !== 'hours' ? '2-digit' : 'numeric';
          dispDefault = digital && (timeish) ? 'always' : 'auto';
          if (digital && sub) dispDefault = 'auto';
        }
        if ((u === 'minutes' || u === 'seconds') && (prev === 'numeric' || prev === '2-digit')) st = '2-digit';
        const disp = getOpt(o, u + 'Display', 'string', ['auto', 'always'], dispDefault, 'Intl.DurationFormat');
        this.u[u] = [st, disp];
        prev = st;
      }
      this.fractionalDigits = getNum(o, 'fractionalDigits', 0, 9, undefined);
      this.d = LD(this.key);
    }
    resolved() {
      const r = { locale: this.locale, numberingSystem: this.numberingSystem, style: this.style };
      for (const u of DUR_UNITS) { r[u] = this.u[u][0]; r[u + 'Display'] = this.u[u][1]; }
      if (this.fractionalDigits !== undefined) r.fractionalDigits = this.fractionalDigits;
      return r;
    }
    parts(dur) {
      if (dur === null || (typeof dur !== 'object' && typeof dur !== 'string')) throw typeError('Invalid duration');
      if (typeof dur === 'string') throw rangeError('Invalid duration string');
      const v = {};
      let any = false, sign = 0;
      for (const u of DUR_UNITS) {
        const x = dur[u];
        if (x === undefined) { v[u] = 0; continue; }
        const n = Number(x);
        if (!Number.isInteger(n)) throw rangeError('Invalid duration value for ' + u);
        v[u] = n; any = true;
        if (n) { const s = Math.sign(n); if (sign && s !== sign) throw rangeError('Mixed-sign duration'); sign = s; }
      }
      if (!any) throw typeError('Invalid duration');
      const loc = this.locale.replace(/-u-.*/, '') + (this.numberingSystem !== 'latn' ? '-u-nu-' + this.numberingSystem : '');
      const items = [];
      // digital clock group: hours:minutes:seconds
      const clock = [];
      for (let i = 0; i < DUR_UNITS.length; i++) {
        const u = DUR_UNITS[i], [st, disp] = this.u[u];
        let val = v[u];
        if (st === 'numeric' || st === '2-digit') {
          if (u === 'milliseconds' || u === 'microseconds' || u === 'nanoseconds') continue;
          if (u === 'seconds') {
            // fold sub-second units into a fraction
            const frac = (Math.abs(v.milliseconds) * 1e6 + Math.abs(v.microseconds) * 1e3 + Math.abs(v.nanoseconds));
            const nf = new NumberFormatImpl(loc, { minimumIntegerDigits: st === '2-digit' ? 2 : 1, minimumFractionDigits: this.fractionalDigits || 0, maximumFractionDigits: this.fractionalDigits === undefined ? 9 : this.fractionalDigits, useGrouping: false });
            const secs = Math.abs(val) + frac / 1e9;
            clock.push(nf.toParts(secs).parts);
            continue;
          }
          const nf = new NumberFormatImpl(loc, { minimumIntegerDigits: st === '2-digit' ? 2 : 1, useGrouping: false });
          if (disp === 'auto' && !val && !clock.length && u === 'hours') continue;
          clock.push(nf.toParts(Math.abs(val)).parts);
          continue;
        }
        if (u === 'seconds' || u === 'milliseconds' || u === 'microseconds') {
          // a numeric next unit folds into this one
          const next = DUR_UNITS[i + 1], nst = this.u[next][0];
          if (nst === 'numeric') {
            const scale = { seconds: [1e3, 1e6, 1e9], milliseconds: [1, 1e3, 1e6], microseconds: [0, 1, 1e3] }[u];
            val = Math.abs(val) + (Math.abs(v.milliseconds) / scale[0] || 0) * (u === 'seconds' ? 1 : 0) + (u === 'seconds' ? Math.abs(v.microseconds) / 1e6 + Math.abs(v.nanoseconds) / 1e9 : u === 'milliseconds' ? Math.abs(v.microseconds) / 1e3 + Math.abs(v.nanoseconds) / 1e6 : Math.abs(v.nanoseconds) / 1e3);
            if (disp === 'auto' && !val) break;
            const nf = new NumberFormatImpl(loc, { style: 'unit', unit: DUR_SINGULAR[u], unitDisplay: st, maximumFractionDigits: this.fractionalDigits === undefined ? 9 : this.fractionalDigits, minimumFractionDigits: this.fractionalDigits || 0 });
            items.push(nf.toParts(sign < 0 && !items.length ? -val : val).parts);
            break;
          }
        }
        if (disp === 'auto' && !val) continue;
        const nf = new NumberFormatImpl(loc, { style: 'unit', unit: DUR_SINGULAR[u], unitDisplay: st });
        items.push(nf.toParts(sign < 0 && !items.length ? -Math.abs(val) : Math.abs(val)).parts);
      }
      if (clock.length) {
        const sep = { type: 'literal', value: ':' };
        const c = [];
        if (sign < 0 && !items.length) c.push({ type: 'minusSign', value: '-' });
        clock.forEach((p, i) => { if (i) c.push(sep); c.push(...p); });
        items.push(c);
      }
      const lstyle = this.style === 'digital' ? 'short' : this.style;
      const lf = new ListFormatImpl(this.locale.replace(/-u-.*/, ''), { type: 'unit', style: lstyle });
      const placeholders = items.map((_, i) => '\u0000' + i + '\u0000');
      const lp = lf.parts(placeholders);
      const out = [];
      for (const p of lp) {
        if (p.type === 'element') out.push(...items[+p.value.slice(1, -1)].map((x) => ({ type: x.type, value: x.value })));
        else out.push({ type: 'literal', value: p.value });
      }
      return out;
    }
    format(d) { return this.parts(d).map((p) => p.value).join(''); }
  }

  // ---------- Intl.Locale ----------
  const LIKELY = {
    en: 'Latn-US', de: 'Latn-DE', fr: 'Latn-FR', es: 'Latn-ES', it: 'Latn-IT', pt: 'Latn-BR', nl: 'Latn-NL', sv: 'Latn-SE', pl: 'Latn-PL', ja: 'Jpan-JP', zh: 'Hans-CN', ko: 'Kore-KR', ru: 'Cyrl-RU',
    uk: 'Cyrl-UA', be: 'Cyrl-BY', bg: 'Cyrl-BG', sr: 'Cyrl-RS', mk: 'Cyrl-MK', kk: 'Cyrl-KZ', ar: 'Arab-EG', fa: 'Arab-IR', ur: 'Arab-PK', he: 'Hebr-IL', yi: 'Hebr-001', hi: 'Deva-IN', mr: 'Deva-IN', ne: 'Deva-NP', bn: 'Beng-BD', ta: 'Taml-IN', te: 'Telu-IN',
    th: 'Thai-TH', el: 'Grek-GR', hy: 'Armn-AM', ka: 'Geor-GE', am: 'Ethi-ET', km: 'Khmr-KH', lo: 'Laoo-LA', my: 'Mymr-MM', si: 'Sinh-LK',
    da: 'Latn-DK', nb: 'Latn-NO', nn: 'Latn-NO', no: 'Latn-NO', fi: 'Latn-FI', is: 'Latn-IS', et: 'Latn-EE', lv: 'Latn-LV', lt: 'Latn-LT', cs: 'Latn-CZ', sk: 'Latn-SK', sl: 'Latn-SI', hr: 'Latn-HR', bs: 'Latn-BA', hu: 'Latn-HU', ro: 'Latn-RO', tr: 'Latn-TR', az: 'Latn-AZ',
    ca: 'Latn-ES', eu: 'Latn-ES', gl: 'Latn-ES', ga: 'Latn-IE', cy: 'Latn-GB', sq: 'Latn-AL', mt: 'Latn-MT', id: 'Latn-ID', ms: 'Latn-MY', vi: 'Latn-VN', fil: 'Latn-PH', sw: 'Latn-TZ', af: 'Latn-ZA', zu: 'Latn-ZA', uz: 'Latn-UZ', und: 'en-Latn-US',
  };
  const LIKELY_REGION_SCRIPT = { 'zh-TW': 'Hant', 'zh-HK': 'Hant', 'zh-MO': 'Hant', 'sr-ME': 'Latn', 'pa-PK': 'Arab' };
  const LIKELY_SCRIPT_REGION = { 'zh-Hant': 'TW', 'sr-Latn': 'RS', 'pa-Arab': 'PK', 'uz-Arab': 'AF', 'az-Arab': 'IR' };
  function maximize(p) {
    const r = Object.assign({}, p, { variants: p.variants.slice(), ext: p.ext });
    let lang = r.lang;
    if (lang === 'und') { const l = LIKELY.und.split('-'); r.lang = lang = l[0]; if (!r.script) r.script = l[1]; if (!r.region) r.region = l[2]; return r; }
    const lk = LIKELY[lang];
    if (!r.script) r.script = LIKELY_REGION_SCRIPT[lang + '-' + r.region] || (lk ? lk.split('-')[0] : '');
    if (!r.region) r.region = LIKELY_SCRIPT_REGION[lang + '-' + r.script] || (lk ? lk.split('-')[1] : '');
    return r;
  }
  function minimize(p) {
    const max = maximize(p);
    const base = { lang: max.lang, script: '', region: '', variants: [], ext: [], priv: '' };
    for (const trial of [{}, { region: max.region }, { script: max.script }]) {
      const t = Object.assign({}, base, trial);
      const m = maximize(t);
      if (m.script === max.script && m.region === max.region) return Object.assign({}, p, { lang: max.lang, script: t.script, region: t.region });
    }
    return Object.assign({}, p, { lang: max.lang, script: max.script, region: max.region });
  }
  const LOCALE_KEYS = { calendar: 'ca', collation: 'co', firstDayOfWeek: 'fw', hourCycle: 'hc', caseFirst: 'kf', numeric: 'kn', numberingSystem: 'nu' };
  const WEEK_SUNDAY = new Set(['US', 'CA', 'MX', 'BR', 'JP', 'KR', 'TW', 'HK', 'IL', 'IN', 'PH', 'ZA', 'AU', 'SA', 'AR', 'CO', 'PE', 'VE', 'GT', 'PR', 'TH', 'ID', 'KE', 'PK']);
  class IntlLocale {
    constructor(tagArg, options) {
      if (typeof tagArg !== 'string' && (typeof tagArg !== 'object' || tagArg === null)) throw typeError('First argument to Intl.Locale constructor can\'t be empty or missing');
      let t = tagArg instanceof IntlLocale ? slot(tagArg, 'Locale').tag : String(tagArg);
      const o = coerceOptions(options);
      const p = parseTag(canonTag(t));
      if (!p) throw rangeError('Incorrect locale information provided');
      const lang = getOpt(o, 'language', 'string', null, undefined);
      if (lang !== undefined) { if (!/^([a-z]{2,3}|[a-z]{5,8})$/i.test(lang)) throw rangeError('Incorrect locale information provided'); p.lang = lang.toLowerCase(); }
      const script = getOpt(o, 'script', 'string', null, undefined);
      if (script !== undefined) { if (!/^[a-z]{4}$/i.test(script)) throw rangeError('Incorrect locale information provided'); p.script = script[0].toUpperCase() + script.slice(1).toLowerCase(); }
      const region = getOpt(o, 'region', 'string', null, undefined);
      if (region !== undefined) { if (!/^([a-z]{2}|\d{3})$/i.test(region)) throw rangeError('Incorrect locale information provided'); p.region = region.toUpperCase(); }
      const kw = {};
      const ue = p.ext.find((e) => e[0] === 'u');
      let attrs = [];
      if (ue) {
        const u = parseU(ue[1]);
        attrs = u[0];
        for (const [k, v] of u[1]) kw[k] = v;
      }
      for (const [opt, k] of Object.entries(LOCALE_KEYS)) {
        let v = opt === 'numeric' ? getOpt(o, opt, 'boolean', null, undefined) : getOpt(o, opt, 'string', opt === 'hourCycle' ? ['h11', 'h12', 'h23', 'h24'] : opt === 'caseFirst' ? ['upper', 'lower', 'false'] : null, undefined, 'Intl.Locale');
        if (v === undefined) continue;
        v = String(v);
        if (opt !== 'numeric' && !/^[a-z0-9]{3,8}(-[a-z0-9]{3,8})*$/i.test(v)) throw rangeError('Incorrect locale information provided');
        kw[k] = v.toLowerCase();
      }
      const keys = Object.keys(kw).sort();
      p.ext = p.ext.filter((e) => e[0] !== 'u');
      if (keys.length || attrs.length) {
        const parts = [...attrs];
        for (const k of keys) { parts.push(k); if (kw[k] && kw[k] !== 'true') parts.push(...kw[k].split('-')); }
        p.ext.push(['u', parts]);
        p.ext.sort((a, b) => (a[0] < b[0] ? -1 : 1));
      }
      const tagS = canonTag(tagToString(p));
      setSlot(this, 'Locale', { tag: tagS, p: parseTag(tagS), kw });
    }
  }
  const L = (o) => slot(o, 'Locale');
  methods(IntlLocale.prototype, {
    toString() { return L(this).tag; },
    toJSON() { return L(this).tag; },
    maximize() { const s = L(this); return new IntlLocale(tagToString(maximize(s.p))); },
    minimize() { const s = L(this); return new IntlLocale(tagToString(minimize(s.p))); },
    get baseName() { const p = L(this).p; return tagToString({ lang: p.lang, script: p.script, region: p.region, variants: p.variants, ext: [], priv: '' }); },
    get language() { const l = L(this).p.lang; return l === 'und' ? undefined : l; },
    get script() { return L(this).p.script || undefined; },
    get region() { return L(this).p.region || undefined; },
    get variants() { const v = L(this).p.variants; return v.length ? v.join('-') : undefined; },
    get calendar() { return L(this).kw.ca; },
    get collation() { return L(this).kw.co; },
    get hourCycle() { return L(this).kw.hc; },
    get caseFirst() { return L(this).kw.kf; },
    get numeric() { const v = L(this).kw.kn; return v === '' || v === 'true'; },
    get numberingSystem() { return L(this).kw.nu; },
    get firstDayOfWeek() { return L(this).kw.fw; },
    getCalendars() { const s = L(this); return [s.kw.ca || 'gregory']; },
    getCollations() { const s = L(this); return [s.kw.co || 'emoji', 'eor'].filter((x, i, a) => a.indexOf(x) === i).sort(); },
    getHourCycles() { const s = L(this); if (s.kw.hc) return [s.kw.hc]; const k = dataKey(L(this).p.lang + (L(this).p.region ? '-' + L(this).p.region : '')); return [k ? LD(k).hc || 'h12' : 'h23']; },
    getNumberingSystems() { const s = L(this); return [s.kw.nu || 'latn']; },
    getTimeZones() { const r = L(this).p.region; if (!r) return undefined; return (REGION_ZONES[r] || '').split(' ').filter(Boolean).sort(); },
    getTextInfo() { return { direction: /^(ar|he|fa|ur|yi|ps|sd|ug|dv|ckb)$/.test(L(this).p.lang) || L(this).p.script === 'Arab' || L(this).p.script === 'Hebr' ? 'rtl' : 'ltr' }; },
    getWeekInfo() {
      const s = L(this), r = maximize(s.p).region;
      const fw = { mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6, sun: 7 }[s.kw.fw];
      const first = fw || (WEEK_SUNDAY.has(r) ? 7 : ['AE', 'AF', 'BH', 'DJ', 'DZ', 'EG', 'IQ', 'IR', 'JO', 'KW', 'LY', 'OM', 'QA', 'SD', 'SY'].includes(r) ? 6 : 1);
      const weekend = ['AE', 'BH', 'DZ', 'EG', 'IQ', 'IL', 'JO', 'KW', 'LY', 'OM', 'QA', 'SA', 'SD', 'SY', 'YE'].includes(r) ? [5, 6] : r === 'IN' ? [7] : r === 'IR' ? [5] : [6, 7];
      return { firstDay: first, weekend };
    },
  });
  for (const k of ['calendars', 'collations', 'hourCycles', 'numberingSystems', 'timeZones', 'textInfo', 'weekInfo']) {
    const m = 'get' + k[0].toUpperCase() + k.slice(1);
    Object.defineProperty(IntlLocale.prototype, k, { get() { return this[m](); }, configurable: true });
  }
  const REGION_ZONES = {
    US: 'America/Adak America/Anchorage America/Boise America/Chicago America/Denver America/Detroit America/Indiana/Knox America/Indiana/Marengo America/Indiana/Petersburg America/Indiana/Tell_City America/Indiana/Vevay America/Indiana/Vincennes America/Indiana/Winamac America/Indianapolis America/Juneau America/Kentucky/Monticello America/Los_Angeles America/Louisville America/Menominee America/Metlakatla America/New_York America/Nome America/North_Dakota/Beulah America/North_Dakota/Center America/North_Dakota/New_Salem America/Phoenix America/Sitka America/Yakutat Pacific/Honolulu',
    GB: 'Europe/London', DE: 'Europe/Berlin Europe/Busingen', FR: 'Europe/Paris', IN: 'Asia/Calcutta', JP: 'Asia/Tokyo', CN: 'Asia/Shanghai Asia/Urumqi', KR: 'Asia/Seoul', IT: 'Europe/Rome', ES: 'Africa/Ceuta Atlantic/Canary Europe/Madrid', NL: 'Europe/Amsterdam', SE: 'Europe/Stockholm', PL: 'Europe/Warsaw', IE: 'Europe/Dublin', CH: 'Europe/Zurich', AT: 'Europe/Vienna',
  };
  tag(IntlLocale.prototype, 'Intl.Locale');

  // ---------- public constructors ----------
  function legacyCtor(name, Impl, extra) {
    const C = function (locales, options) {
      const obj = Object.create((new.target || C).prototype);
      setSlot(obj, name, new Impl(locales, options, ...(extra || [])));
      return obj;
    };
    Object.defineProperty(C, 'name', { value: name });
    return C;
  }
  function strictCtor(name, Impl) {
    const C = function (locales, options) {
      if (!new.target) throw typeError('Constructor Intl.' + name + " requires 'new'");
      const obj = Object.create(new.target.prototype);
      setSlot(obj, name, new Impl(locales, options));
      return obj;
    };
    Object.defineProperty(C, 'name', { value: name });
    return C;
  }
  function setupCtor(C, name, length) {
    Object.defineProperty(C, 'length', { value: length, configurable: true });
    const proto = C.prototype;
    Object.defineProperty(C, 'prototype', { writable: false, enumerable: false, configurable: false });
    Object.defineProperty(proto, 'constructor', { value: C, writable: true, configurable: true, enumerable: false });
    tag(proto, 'Intl.' + name);
    def(C, 'supportedLocalesOf', { supportedLocalesOf(locales, options) { return supportedLocalesOf(locales, options); } }.supportedLocalesOf);
    return proto;
  }
  const bound = (s, k, f) => s[k] || (s[k] = f);

  // DateTimeFormat
  const DateTimeFormat = legacyCtor('DateTimeFormat', DateTimeFormatImpl, ['any', 'date']);
  methods(setupCtor(DateTimeFormat, 'DateTimeFormat', 0), {
    get format() { const s = slot(this, 'DateTimeFormat'); return bound(s, 'boundFormat', (date) => s.format(toDateValue(date))); },
    formatToParts(date) { return slot(this, 'DateTimeFormat').parts(toDateValue(date)); },
    formatRange(a, b) {
      const s = slot(this, 'DateTimeFormat');
      if (a === undefined || b === undefined) throw typeError('Invalid time value');
      return s.rangeParts(toDateValue(a), toDateValue(b)).map((p) => p.value).join('');
    },
    formatRangeToParts(a, b) {
      const s = slot(this, 'DateTimeFormat');
      if (a === undefined || b === undefined) throw typeError('Invalid time value');
      return s.rangeParts(toDateValue(a), toDateValue(b));
    },
    resolvedOptions() { return slot(this, 'DateTimeFormat').resolved(); },
  });
  for (const k of ['format', 'formatToParts', 'formatRange', 'formatRangeToParts', 'resolvedOptions']) { const d = Object.getOwnPropertyDescriptor(DateTimeFormat.prototype, k); if (d.value) Object.defineProperty(d.value, 'name', { value: k }); }

  // NumberFormat
  const NumberFormat = legacyCtor('NumberFormat', NumberFormatImpl);
  methods(setupCtor(NumberFormat, 'NumberFormat', 0), {
    get format() { const s = slot(this, 'NumberFormat'); return bound(s, 'boundFormat', (x) => s.format(toIntlMV(x))); },
    formatToParts(x) { return slot(this, 'NumberFormat').toParts(toIntlMV(x)).parts; },
    formatRange(a, b) {
      const s = slot(this, 'NumberFormat');
      if (a === undefined || b === undefined) throw typeError('start or end is undefined');
      const x = toIntlMV(a), y = toIntlMV(b);
      if (Number.isNaN(x) || Number.isNaN(y)) throw rangeError('Invalid range');
      return s.rangeParts(x, y).map((p) => p.value).join('');
    },
    formatRangeToParts(a, b) {
      const s = slot(this, 'NumberFormat');
      if (a === undefined || b === undefined) throw typeError('start or end is undefined');
      const x = toIntlMV(a), y = toIntlMV(b);
      if (Number.isNaN(x) || Number.isNaN(y)) throw rangeError('Invalid range');
      return s.rangeParts(x, y);
    },
    resolvedOptions() { return slot(this, 'NumberFormat').resolved(); },
  });

  // Collator
  const Collator = legacyCtor('Collator', CollatorImpl);
  methods(setupCtor(Collator, 'Collator', 0), {
    get compare() { const s = slot(this, 'Collator'); return bound(s, 'boundCompare', (a, b) => s.compare(a, b)); },
    resolvedOptions() { return slot(this, 'Collator').resolved(); },
  });

  // PluralRules
  const PluralRules = strictCtor('PluralRules', PluralRulesImpl);
  methods(setupCtor(PluralRules, 'PluralRules', 0), {
    select(n) { return slot(this, 'PluralRules').select(n); },
    selectRange(a, b) {
      const s = slot(this, 'PluralRules');
      if (a === undefined || b === undefined) throw typeError('start or end is undefined');
      if (Number.isNaN(Number(a)) || Number.isNaN(Number(b))) throw rangeError('Invalid range');
      return s.select(b);
    },
    resolvedOptions() { return slot(this, 'PluralRules').resolved(); },
  });

  // RelativeTimeFormat
  const RelativeTimeFormat = strictCtor('RelativeTimeFormat', RelativeTimeFormatImpl);
  methods(setupCtor(RelativeTimeFormat, 'RelativeTimeFormat', 0), {
    format(v, u) { return slot(this, 'RelativeTimeFormat').format(v, u); },
    formatToParts(v, u) { return slot(this, 'RelativeTimeFormat').parts(v, u); },
    resolvedOptions() { return slot(this, 'RelativeTimeFormat').resolved(); },
  });

  // ListFormat
  const ListFormat = strictCtor('ListFormat', ListFormatImpl);
  methods(setupCtor(ListFormat, 'ListFormat', 0), {
    format(list) { return slot(this, 'ListFormat').format(list); },
    formatToParts(list) { return slot(this, 'ListFormat').parts(list); },
    resolvedOptions() { return slot(this, 'ListFormat').resolved(); },
  });

  // Segmenter
  const Segmenter = strictCtor('Segmenter', SegmenterImpl);
  const SegmentsProto = {};
  const SegmentIteratorProto = Object.create(Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]())));
  function segmentData(s, str, seg) {
    const o = { segment: str.slice(seg[0], seg[1]), index: seg[0], input: str };
    if (s.granularity === 'word') o.isWordLike = seg[2];
    return o;
  }
  methods(SegmentsProto, {
    containing(index) {
      const st = slot(this, 'Segments');
      let n = Math.trunc(Number(index)) || 0;
      if (index === undefined) n = 0;
      if (n < 0 || n >= st.str.length) return undefined;
      const segs = st.segs || (st.segs = st.seg.segmentsOf(st.str));
      let lo = 0, hi = segs.length - 1;
      while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (segs[mid][0] <= n) lo = mid; else hi = mid - 1; }
      return segmentData(st.seg, st.str, segs[lo]);
    },
  });
  def(SegmentsProto, Symbol.iterator, function () {
    const st = slot(this, 'Segments');
    const it = Object.create(SegmentIteratorProto);
    setSlot(it, 'SegmentIterator', { st, i: 0 });
    return it;
  });
  methods(SegmentIteratorProto, {
    next() {
      const s = slot(this, 'SegmentIterator');
      const st = s.st;
      const segs = st.segs || (st.segs = st.seg.segmentsOf(st.str));
      if (s.i >= segs.length) return { value: undefined, done: true };
      return { value: segmentData(st.seg, st.str, segs[s.i++]), done: false };
    },
  });
  tag(SegmentIteratorProto, 'Segmenter String Iterator');
  methods(setupCtor(Segmenter, 'Segmenter', 0), {
    segment(str) {
      const s = slot(this, 'Segmenter');
      const o = Object.create(SegmentsProto);
      setSlot(o, 'Segments', { seg: s, str: String(str), segs: null });
      return o;
    },
    resolvedOptions() { return slot(this, 'Segmenter').resolved(); },
  });

  // DisplayNames
  const DisplayNames = strictCtor('DisplayNames', DisplayNamesImpl);
  methods(setupCtor(DisplayNames, 'DisplayNames', 2), {
    of(code) { return slot(this, 'DisplayNames').of(code); },
    resolvedOptions() { return slot(this, 'DisplayNames').resolved(); },
  });

  // DurationFormat
  const DurationFormat = strictCtor('DurationFormat', DurationFormatImpl);
  methods(setupCtor(DurationFormat, 'DurationFormat', 0), {
    format(d) { return slot(this, 'DurationFormat').format(d); },
    formatToParts(d) { return slot(this, 'DurationFormat').parts(d); },
    resolvedOptions() { return slot(this, 'DurationFormat').resolved(); },
  });

  // Locale
  Object.defineProperty(IntlLocale, 'name', { value: 'Locale' });
  Object.defineProperty(IntlLocale, 'length', { value: 1 });

  // ---------- Intl ----------
  const Intl = {};
  tag(Intl, 'Intl');
  def(Intl, 'getCanonicalLocales', { getCanonicalLocales(locales) { return canonList(locales); } }.getCanonicalLocales);
  def(Intl, 'supportedValuesOf', { supportedValuesOf(key) {
    switch (String(key)) {
      case 'calendar': return ['gregory'];
      case 'collation': return ['emoji', 'eor'];
      case 'currency': return CURRENCIES.split(' ');
      case 'numberingSystem': return Object.keys(NUMBERING).sort();
      case 'timeZone': return ZONES.split(' ').filter((z) => !/^Etc\/|^(UTC|GMT)$/.test(z) || z === 'UTC');
      case 'unit': return UNITS.split(' ').sort();
      default: throw rangeError('Invalid key : ' + String(key));
    }
  } }.supportedValuesOf);
  for (const [k, v] of Object.entries({ DateTimeFormat, NumberFormat, Collator, PluralRules, RelativeTimeFormat, ListFormat, Locale: IntlLocale, DisplayNames, Segmenter, DurationFormat })) def(Intl, k, v);
  def(G, 'Intl', Intl);

  // ---------- toLocale* ----------
  const fmtCache = new Map();
  function cached(kind, locales, options, make) {
    let key;
    try { key = kind + '\u0000' + (locales === undefined ? '' : typeof locales === 'string' ? locales : JSON.stringify(locales)) + '\u0000' + (options === undefined ? '' : JSON.stringify(options)); } catch { key = null; }
    if (key !== null && (options === undefined || Object.getPrototypeOf(options) === Object.prototype || Object.getPrototypeOf(options) === null)) {
      let f = fmtCache.get(key);
      if (!f) {
        f = make();
        if (fmtCache.size > 300) fmtCache.clear();
        fmtCache.set(key, f);
      }
      return f;
    }
    return make();
  }
  const dateValue = (d) => Date.prototype.getTime.call(d);
  const dateMethod = (name, req, defs) => ({
    [name](locales, options) {
      const t = dateValue(this);
      if (Number.isNaN(t)) return 'Invalid Date';
      return cached(name, locales, options, () => new DateTimeFormatImpl(locales, options, req, defs)).format(t);
    },
  }[name]);
  def(Date.prototype, 'toLocaleString', dateMethod('toLocaleString', 'any', 'all'));
  def(Date.prototype, 'toLocaleDateString', dateMethod('toLocaleDateString', 'date', 'date'));
  def(Date.prototype, 'toLocaleTimeString', dateMethod('toLocaleTimeString', 'time', 'time'));
  const numValue = Number.prototype.valueOf, bigValue = BigInt.prototype.valueOf;
  def(Number.prototype, 'toLocaleString', { toLocaleString(locales, options) { const x = numValue.call(this); return cached('n', locales, options, () => new NumberFormatImpl(locales, options)).format(x); } }.toLocaleString);
  def(BigInt.prototype, 'toLocaleString', { toLocaleString(locales, options) { const x = bigValue.call(this); return cached('n', locales, options, () => new NumberFormatImpl(locales, options)).format(x); } }.toLocaleString);
  def(String.prototype, 'localeCompare', { localeCompare(that, locales, options) {
    if (this === null || this === undefined) throw typeError('String.prototype.localeCompare called on null or undefined');
    const s = String(this), t = String(that);
    return cached('c', locales, options, () => new CollatorImpl(locales, options)).compare(s, t);
  } }.localeCompare);
  Object.defineProperty(String.prototype.localeCompare, 'length', { value: 1 });
  def(Array.prototype, 'toLocaleString', { toLocaleString(locales, options) {
    const O = Object(this), len = Math.min(Number(O.length) || 0, 2 ** 32);
    let r = '';
    for (let k = 0; k < len; k++) {
      if (k) r += ',';
      const e = O[k];
      if (e !== undefined && e !== null) r += String(Object(e).toLocaleString(locales, options));
    }
    return r;
  } }.toLocaleString);
  const upper = String.prototype.toUpperCase, lower = String.prototype.toLowerCase;
  const caseLang = (locales) => {
    const l = canonList(locales);
    const t = l.length ? l[0] : defaultLocale();
    return t.split('-')[0];
  };
  def(String.prototype, 'toLocaleUpperCase', { toLocaleUpperCase(locales) {
    if (this === null || this === undefined) throw typeError('String.prototype.toLocaleUpperCase called on null or undefined');
    const s = String(this), l = caseLang(locales);
    if (l === 'tr' || l === 'az') return upper.call(s.replace(/i/g, '\u0130'));
    if (l === 'lt') return upper.call(s.replace(/([ij\u012F])\u0307/g, '$1'));
    return upper.call(s);
  } }.toLocaleUpperCase);
  def(String.prototype, 'toLocaleLowerCase', { toLocaleLowerCase(locales) {
    if (this === null || this === undefined) throw typeError('String.prototype.toLocaleLowerCase called on null or undefined');
    const s = String(this), l = caseLang(locales);
    if (l === 'tr' || l === 'az') return lower.call(s.replace(/\u0130/g, 'i').replace(/I(?!\u0307)/g, 'ı').replace(/I\u0307/g, 'i'));
    return lower.call(s);
  } }.toLocaleLowerCase);
})();
