import { expectAccessibleInBothSchemes } from '../support/a11y';
import { expect, openShorts, routeYouTube, test, tokyoClock } from '../support/extension';

test('watching Shorts before bedtime shows one quiet bedtime reminder per night', async ({
  context,
  api,
  app,
}) => {
  test.setTimeout(90_000);
  // 就寝の30分前（夜の時間帯は就寝の1時間前から）。
  await api.setSleep(tokyoClock(30 * 60_000), tokyoClock(8.5 * 3_600_000));
  await app.reload();
  await routeYouTube(context, { autoplay: false });
  const { page: youtube, overlay } = await openShorts(context, 'dopagaki-night');
  const reminder = overlay.getByRole('dialog', { name: '寝る時間が近づいています' });
  const heading = reminder.getByRole('heading', { name: 'そろそろ、寝る準備。' });
  // 接続が終わる前に開いたときは、開き直したときに出す。
  await expect(async () => {
    if (!(await heading.isVisible())) await youtube.reload();
    await expect(heading).toBeVisible({ timeout: 5_000 });
  }).toPass({ timeout: 40_000 });
  await expect(reminder).toContainText('寝る準備');
  await expect(reminder.getByText('このまま寝ると、朝に視聴時間 最大15分')).toBeVisible();
  await expectAccessibleInBothSchemes(youtube, { include: ['#dopagaki-extension'] });

  await reminder.getByRole('button', { name: 'あと少しだけ', exact: true }).click();
  await expect(heading).toBeHidden();
  // 同じ夜には二度出さない。
  await youtube.reload();
  await youtube.waitForTimeout(3_000);
  await expect(heading).toBeHidden();
});
