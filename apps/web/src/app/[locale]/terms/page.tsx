"use client";

import { use } from "react";

import { LEGAL, LegalPage, type LegalDoc } from "@/components/meals/legal";
import { asLocale } from "@/components/meals/shell";

const E = LEGAL.entity;
const M = LEGAL.email;

const DOC: LegalDoc = {
  en: {
    title: "Terms of use",
    updated: "Last updated",
    sections: [
      { h: "About these terms", p: [`These terms apply when you use the meals marketplace run by ${E} ("we"), on the web or in the app. By signing in you agree to them. You must be 18 or over.`] },
      { h: "What we do", p: [
        "We list independent kitchens and let you subscribe to their meal plans. Each kitchen prepares its own food and is responsible for its ingredients, allergens, hygiene and food-safety licence. We check licences before showing the \"food safety verified\" badge but do not cook the food.",
        "Menus, photos and allergen notes come from the kitchen. If you have an allergy, check with the kitchen before you subscribe.",
      ] },
      { h: "Subscriptions and payment", p: [
        "A plan covers a set number of meals over its cycle, at the price shown at checkout including VAT. You pay for each cycle in advance.",
        "Plans with auto-renew on renew at the end of each cycle at the then-current price; you can turn auto-renew off in My meals at any time before renewal.",
        "Dish choices and skips must be made before the kitchen's cut-off time shown in the app; after that the default dish is delivered.",
      ] },
      { h: "Pausing, cancelling and refunds", p: [
        "You can pause or cancel a subscription in My meals. Meals already prepared or past their cut-off are charged. Any unused balance for future deliveries is refunded or credited as shown when you cancel.",
        "If a meal does not arrive or is clearly not as described, report it in the app within 24 hours and we will credit or refund it.",
      ] },
      { h: "Delivery", p: ["Keep your address and phone number correct, and be reachable in the delivery window. A meal that cannot be delivered because the address was wrong or nobody was available is not refunded."] },
      { h: "Your account", p: [
        "You sign in with a one-time code sent to your phone; keep it to yourself. You are responsible for activity on your account.",
        "Do not misuse the service: no fake orders, abuse of kitchens or drivers, false reviews, or attempts to break or scrape the service. We may suspend accounts that do.",
        "You can delete your account at any time from the Me screen.",
      ] },
      { h: "Reviews and content", p: ["Ratings and reviews must be honest and about your own experience. We may remove content that is unlawful, abusive or misleading."] },
      { h: "Liability", p: ["We are responsible for running the marketplace with reasonable care. Kitchens are responsible for the food they prepare. Nothing in these terms limits rights you have under UAE consumer protection law."] },
      { h: "Changes and law", p: [
        "We may update these terms and will tell you in the app before a significant change takes effect.",
        "These terms are governed by the laws of the United Arab Emirates and the courts of Dubai.",
      ] },
      { h: "Contact", p: [`${E} · ${M}`] },
    ],
  },
  ar: {
    title: "شروط الاستخدام",
    updated: "آخر تحديث",
    sections: [
      { h: "عن هذه الشروط", p: [`تنطبق هذه الشروط عند استخدامك سوق الوجبات الذي تديره ${E} («نحن») على الويب أو في التطبيق. بتسجيل الدخول فإنك توافق عليها. يجب أن يكون عمرك 18 عامًا أو أكثر.`] },
      { h: "ما الذي نقدمه", p: [
        "نعرض مطابخ مستقلة ونتيح لك الاشتراك في خطط وجباتها. كل مطبخ يحضّر طعامه بنفسه وهو مسؤول عن مكوناته ومسببات الحساسية والنظافة وترخيص سلامة الغذاء. نتحقق من التراخيص قبل عرض شارة «سلامة الغذاء موثّقة» لكننا لا نطبخ الطعام.",
        "القوائم والصور وملاحظات الحساسية مصدرها المطبخ. إن كانت لديك حساسية فتأكد مع المطبخ قبل الاشتراك.",
      ] },
      { h: "الاشتراكات والدفع", p: [
        "تغطي الخطة عددًا محددًا من الوجبات خلال دورتها، بالسعر المعروض عند الدفع شاملًا ضريبة القيمة المضافة. تدفع كل دورة مقدمًا.",
        "الخطط المفعّل فيها التجديد التلقائي تتجدد في نهاية كل دورة بالسعر الساري حينها، ويمكنك إيقاف التجديد التلقائي من «وجباتي» في أي وقت قبل التجديد.",
        "يجب اختيار الأطباق وتخطي الأيام قبل موعد الإغلاق الخاص بالمطبخ الظاهر في التطبيق، وبعده يُوصَّل الطبق الافتراضي.",
      ] },
      { h: "الإيقاف المؤقت والإلغاء والاسترداد", p: [
        "يمكنك إيقاف الاشتراك مؤقتًا أو إلغاؤه من «وجباتي». تُحتسب الوجبات التي حُضّرت أو تجاوزت موعد الإغلاق. يُسترد أي رصيد غير مستخدم للتوصيلات القادمة أو يُضاف كرصيد كما يظهر عند الإلغاء.",
        "إن لم تصل وجبة أو كانت مختلفة بوضوح عن وصفها، أبلغ عنها في التطبيق خلال 24 ساعة وسنضيف رصيدًا أو نرد المبلغ.",
      ] },
      { h: "التوصيل", p: ["احرص على صحة عنوانك ورقم هاتفك، وكن متاحًا خلال وقت التوصيل. لا تُسترد قيمة وجبة تعذّر توصيلها بسبب خطأ في العنوان أو عدم وجود أحد لاستلامها."] },
      { h: "حسابك", p: [
        "تسجّل الدخول برمز لمرة واحدة يُرسل إلى هاتفك، فلا تشاركه مع أحد. أنت مسؤول عن النشاط في حسابك.",
        "لا تسئ استخدام الخدمة: لا طلبات وهمية ولا إساءة للمطابخ أو السائقين ولا تقييمات كاذبة ولا محاولات لاختراق الخدمة أو نسخ محتواها. قد نعلّق الحسابات المخالفة.",
        "يمكنك حذف حسابك في أي وقت من شاشة «أنا».",
      ] },
      { h: "التقييمات والمحتوى", p: ["يجب أن تكون التقييمات صادقة وعن تجربتك أنت. قد نزيل المحتوى المخالف للقانون أو المسيء أو المضلل."] },
      { h: "المسؤولية", p: ["نحن مسؤولون عن تشغيل السوق بعناية معقولة، والمطابخ مسؤولة عن الطعام الذي تحضّره. لا شيء في هذه الشروط يحدّ من حقوقك بموجب قانون حماية المستهلك الإماراتي."] },
      { h: "التغييرات والقانون", p: [
        "قد نحدّث هذه الشروط وسنخبرك في التطبيق قبل أن يسري أي تغيير جوهري.",
        "تخضع هذه الشروط لقوانين دولة الإمارات العربية المتحدة ولمحاكم دبي.",
      ] },
      { h: "التواصل", p: [`${E} · ${M}`] },
    ],
  },
};

export default function TermsPage({ params }: { params: Promise<{ locale: string }> }) {
  return <LegalPage locale={asLocale(use(params).locale)} doc={DOC} />;
}
