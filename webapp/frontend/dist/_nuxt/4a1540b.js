(window.webpackJsonp=window.webpackJsonp||[]).push([[26],{271:function(e,n,t){"use strict";function r(e){if("liberal-arts"===e)return"一般教養";if("major-subjects"===e)return"専門";return""}function c(e,n){switch(e){case"monday":return"月".concat(n);case"tuesday":return"火".concat(n);case"wednesday":return"水".concat(n);case"thursday":return"木".concat(n);case"friday":return"金".concat(n);default:return""}}t.d(n,"b",(function(){return r})),t.d(n,"a",(function(){return c}))},284:function(e,n,t){"use strict";t.r(n);var r=t(6),c=(t(43),t(0)),o=t(271),d=c.a.extend({middleware:"is_loggedin",asyncData:function(e){return Object(r.a)(regeneratorRuntime.mark((function n(){var t,r,c;return regeneratorRuntime.wrap((function(n){for(;;)switch(n.prev=n.next){case 0:return t=e.params.id,n.next=3,e.$axios.get("/api/syllabus/".concat(t));case 3:return r=n.sent,c=r.data,n.abrupt("return",{course:c});case 6:case"end":return n.stop()}}),n)})))()},data:function(){return{course:{id:"",code:"",type:"liberal-arts",name:"",description:"",credit:0,period:0,dayOfWeek:"monday",teacher:"",keywords:""}}},computed:{courseType:function(){return Object(o.b)(this.course.type)},coursePeriod:function(){return Object(o.a)(this.course.dayOfWeek,this.course.period)}}}),l=t(20),component=Object(l.a)(d,(function(){var e=this,n=e.$createElement,t=e._self._c||n;return t("div",[t("div",{staticClass:"py-10 px-8 bg-white shadow-lg w-8/12"},[t("div",{staticClass:"flex-1 flex-col"},[t("section",[t("h1",{staticClass:"text-2xl"},[e._v("科目概要")]),e._v(" "),t("div",{staticClass:"grid grid-cols-syllabus justify-items-stretch items-stretch"},[e._m(0),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.name)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            科目番号\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.code)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            科目種別\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.courseType)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            単位数\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.credit)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            時限\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.coursePeriod)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            担当教員\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.teacher)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            講義内容\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.description)+"\n          ")]),e._v(" "),t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[e._v("\n            キーワード\n          ")]),e._v(" "),t("div",{staticClass:"px-2 py-2 border"},[e._v("\n            "+e._s(e.course.keywords)+"\n          ")])])])])])])}),[function(){var e=this,n=e.$createElement,t=e._self._c||n;return t("div",{staticClass:"\n              px-2\n              py-2\n              bg-primary-500\n              text-white\n              flex flex-col\n              justify-center\n              items-center\n              border\n            "},[t("span",{staticClass:"text-center"},[e._v("科目名")])])}],!1,null,null,null);n.default=component.exports}}]);