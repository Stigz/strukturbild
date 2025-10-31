const React = window.React;

export default function DetailsRail({ details }) {
  if (!details || details.length === 0) {
    return (
      <aside className="story-details-rail">
        <h3>Zitate</h3>
        <div>Keine Zitate für diesen Abschnitt.</div>
      </aside>
    );
  }
  return (
    <aside className="story-details-rail">
      <h3>Zitate</h3>
      {details.map((detail) => (
        <div key={detail.detailId} className="story-detail-item">
          <strong>
            [{detail.transcriptId} {detail.startMinute}–{detail.endMinute}]
          </strong>
          <div>{detail.text}</div>
        </div>
      ))}
    </aside>
  );
}
