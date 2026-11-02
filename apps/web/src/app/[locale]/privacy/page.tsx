"use client";

import { use } from "react";

import { LEGAL, LegalPage, type LegalDoc } from "@/components/meals/legal";
import { asLocale } from "@/components/meals/shell";

const E = LEGAL.entity;
const M = LEGAL.email;

const DOC: LegalDoc = {
  en: {
    title: "Privacy policy",
    updated: "Last updated",
    sections: [
      { h: "Who we are", p: [`${E} runs this meal-subscription marketplace in the UAE, connecting you with independent home-style kitchens. We are responsible for the personal data described here, under the UAE Personal Data Protection Law (Federal Decree-Law No. 45 of 2021).`] },
      { h: "What we collect", p: [
        "Your mobile number, which is how you sign in, and your name if you give it.",
        "Delivery addresses and, when you allow it, your device location, to show kitchens that deliver to you.",
        "Your subscriptions, dish choices, deliveries, ratings, invoices and payments.",
        "Device details needed to keep you signed in and send notifications (device type, app version, push token).",
        "Messages you send to kitchens or to support.",
      ] },
      { h: "Why we use it", p: [
        "To deliver your meals: kitchens and drivers see your name, number and delivery address for the orders they handle.",
        "To sign you in (we text you a one-time code), keep your account secure and prevent fraud.",
        "To send service messages by SMS, WhatsApp, push or email: tomorrow's menu, reminders, delivery updates, renewals. Marketing is sent only if you agree, and you can turn it off at any time.",
        "To keep invoices and accounting records that UAE law requires.",
      ] },
      { h: "Who we share it with", p: [
        "The kitchens you subscribe to and the drivers who deliver for them, for those orders only.",
        "Service providers that run parts of the service for us: hosting, SMS, WhatsApp (Meta), push notifications (Google, Apple), payments and crash reporting. They may only use your data to provide that service.",
        "Authorities, when the law requires it. We do not sell your personal data.",
      ] },
      { h: "Where it is kept and for how long", p: [
        "Your data is hosted in the UAE. Some providers above may process it abroad, with safeguards the law requires.",
        "We keep your account data while your account is open. Invoices and payment records are kept for as long as UAE tax and commercial law requires, even after your account is deleted.",
      ] },
      { h: "Your rights", p: [
        "You can see and correct your details in the app, turn notifications off in Notification settings, and delete your account from the Me screen at any time. Deleting your account cancels your subscriptions, signs you out everywhere and removes your name, number and addresses; invoices are kept without them.",
        `You can also ask us for a copy of your data or object to how we use it by writing to ${M}. You may complain to the UAE Data Office.`,
      ] },
      { h: "Children", p: ["The service is for people aged 18 and over."] },
      { h: "Changes", p: ["We will tell you in the app before a significant change to this policy takes effect."] },
      { h: "Contact", p: [`${E} · ${M}`] },
    ],
  },
  ar: {
    title: "سياسة الخصوصية",
    updated: "آخر تحديث",
    sections: [
      { h: "من نحن", p: [`تدير ${E} سوق اشتراكات الوجبات هذا في الإمارات، ويصلك بمطابخ منزلية مستقلة. نحن مسؤولون عن البيانات الشخصية الموضحة هنا وفق قانون حماية البيانات الشخصية الإماراتي (المرسوم بقانون اتحادي رقم 45 لسنة 2021).`] },
      { h: "ما الذي نجمعه", p: [
        "رقم هاتفك الذي تسجّل الدخول به، واسمك إن أعطيتنا إياه.",
        "عناوين التوصيل، وموقع جهازك عندما تسمح بذلك، لنعرض المطابخ التي توصل إليك.",
        "اشتراكاتك واختياراتك من الأطباق وطلبات التوصيل والتقييمات والفواتير والمدفوعات.",
        "بيانات الجهاز اللازمة لإبقائك مسجّلًا وإرسال الإشعارات (نوع الجهاز وإصدار التطبيق ورمز الإشعارات).",
        "الرسائل التي ترسلها إلى المطابخ أو إلى الدعم.",
      ] },
      { h: "لماذا نستخدمها", p: [
        "لتوصيل وجباتك: يرى المطبخ والسائق اسمك ورقمك وعنوان التوصيل للطلبات التي يتولونها فقط.",
        "لتسجيل دخولك (نرسل لك رمزًا لمرة واحدة) وحماية حسابك ومنع الاحتيال.",
        "لإرسال رسائل الخدمة عبر الرسائل النصية أو واتساب أو الإشعارات أو البريد: قائمة الغد والتذكيرات وتحديثات التوصيل والتجديد. لا نرسل رسائل تسويقية إلا بموافقتك، ويمكنك إيقافها في أي وقت.",
        "للاحتفاظ بالفواتير والسجلات المحاسبية التي يفرضها القانون الإماراتي.",
      ] },
      { h: "مع من نشاركها", p: [
        "المطابخ التي تشترك فيها والسائقون الذين يوصلون لها، لتلك الطلبات فقط.",
        "مزودو الخدمات الذين يشغّلون أجزاء من الخدمة لنا: الاستضافة والرسائل النصية وواتساب (ميتا) والإشعارات (جوجل وآبل) والمدفوعات وتقارير الأعطال. لا يحق لهم استخدام بياناتك إلا لتقديم تلك الخدمة.",
        "الجهات الرسمية عندما يفرض القانون ذلك. نحن لا نبيع بياناتك الشخصية.",
      ] },
      { h: "أين تُحفظ وإلى متى", p: [
        "تُستضاف بياناتك في الإمارات. قد يعالجها بعض المزودين أعلاه خارجها، مع الضمانات التي يفرضها القانون.",
        "نحتفظ ببيانات حسابك ما دام حسابك مفتوحًا. تُحفظ الفواتير وسجلات الدفع للمدة التي يفرضها القانون الضريبي والتجاري الإماراتي، حتى بعد حذف حسابك.",
      ] },
      { h: "حقوقك", p: [
        "يمكنك رؤية بياناتك وتصحيحها في التطبيق، وإيقاف الإشعارات من إعدادات الإشعارات، وحذف حسابك من شاشة «أنا» في أي وقت. حذف الحساب يلغي اشتراكاتك ويسجّل خروجك من كل الأجهزة ويزيل اسمك ورقمك وعناوينك، وتبقى الفواتير بدونها.",
        `يمكنك أيضًا طلب نسخة من بياناتك أو الاعتراض على طريقة استخدامها بمراسلتنا على ${M}. ويحق لك تقديم شكوى إلى مكتب الإمارات للبيانات.`,
      ] },
      { h: "الأطفال", p: ["الخدمة مخصصة لمن هم في الثامنة عشرة فما فوق."] },
      { h: "التغييرات", p: ["سنخبرك في التطبيق قبل أن يسري أي تغيير جوهري على هذه السياسة."] },
      { h: "التواصل", p: [`${E} · ${M}`] },
    ],
  },
};

export default function PrivacyPage({ params }: { params: Promise<{ locale: string }> }) {
  return <LegalPage locale={asLocale(use(params).locale)} doc={DOC} />;
}
