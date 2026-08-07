# Arazi Çıkış Cetveli

Windows üzerinde çalışan, aylık arazi çıkış kayıtlarının tutulması için geliştirilmiş masaüstü uygulaması.

Uygulama; yıl ve ay bazlı kayıt oluşturmayı, hafta sonlarını otomatik işaretlemeyi, kayıtları SQLite veritabanında saklamayı ve aylık cetveli PDF olarak oluşturmayı sağlar.

## Özellikler

- Yıl ve ay bazlı kayıt sistemi
- Seçilen yıla göre ayların otomatik oluşturulması
- Tarihlerin takvimden otomatik hesaplanması
- Hafta içlerinin normal beyaz görünmesi
- Boş hafta sonlarının koyu renkle işaretlenmesi
- Hafta sonuna veri girildiğinde satırın otomatik normal görünüme dönmesi
- Araç plakası kaydı
- Göreve gidilen yer kaydı
- Görev konusu kaydı
- Açıklama alanı
- Veteriner hekim bilgilerinin değiştirilebilmesi
- İlçe müdürü bilgilerinin değiştirilebilmesi
- SQLite veritabanı
- Otomatik veri kaydı
- Otomatik ve manuel yedekleme
- A4 yatay tek sayfa PDF çıktısı
- İnternet bağlantısı gerektirmeden çalışma
- Windows uygulama ikonu

## Ekran Görüntüsü

> Uygulama ekran görüntüsü buraya eklenecektir.

## İndirme

Uygulamayı kullanmak için kaynak kodu indirmenize gerek yoktur.

GitHub sayfasındaki **Releases** bölümünden en güncel Windows sürümünü indirebilirsiniz.

Son sürüm:

**Arazi Çıkış Cetveli v1.0.0**

İndirilecek dosya:

`Arazi.Cikis.Cetveli.exe`

## Kullanım

1. Releases bölümünden `.exe` dosyasını indirin.
2. Uygulamayı çalıştırın.
3. Kullanmak istediğiniz yılı seçin.
4. Üst bölümden ayı seçin.
5. İlgili güne ait bilgileri tabloya girin.
6. Girilen bilgiler otomatik olarak kaydedilir.
7. PDF butonu ile seçili ayın cetveli oluşturulabilir.

## Veri Saklama

Uygulama kayıtları programın bulunduğu klasörde tutulmaz.

Windows üzerinde kullanıcıya özel veri klasörü kullanılır:

```text
%APPDATA%\AraziCikisCetveli
